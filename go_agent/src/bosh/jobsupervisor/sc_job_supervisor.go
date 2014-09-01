package jobsupervisor

import (
	//	boshalert "bosh/agent/alert"
	bosherr "bosh/errors"
	boshlog "bosh/logger"
	boshdir "bosh/settings/directories"
	boshsys "bosh/system"

	"code.google.com/p/winsvc/mgr"
	"code.google.com/p/winsvc/svc"
	"encoding/xml"
	"fmt"
	//"github.com/pivotal/go-smtpd/smtpd"
	"path/filepath"
	"sync"
	"time"
)

const jobSupervisorLogTag = "jobSupervisor"
const jobSupervisorPath = "c:\\vcap\\monit\\jobs.xml"

var serviceArguments = []string{}

type jobSupervisor struct {
	fs          boshsys.FileSystem
	runner      boshsys.CmdRunner
	logger      boshlog.Logger
	dirProvider boshdir.DirectoriesProvider

	jobFailuresServerPort int

	reloadOptions ReloadOptions
}

type ReloadOptions struct {
	// Number of times reload will be executed
	MaxTries int

	// Number of times monit incarnation will be checked
	// for difference after executing `monit reload`
	MaxCheckTries int

	// Length of time between checking for incarnation difference
	DelayBetweenCheckTries time.Duration
}

func NewJobSupervisor(
	fs boshsys.FileSystem,
	runner boshsys.CmdRunner,
	logger boshlog.Logger,
	dirProvider boshdir.DirectoriesProvider,
	jobFailuresServerPort int,
	reloadOptions ReloadOptions,
) (js jobSupervisor) {

	go func() {
		for {
			err := CheckAndSync(fs, dirProvider.MonitJobsDir())
			if err != nil {
				logger.Debug("Check and sync", "Error syncronizing services")
			}
			time.Sleep(10 * time.Second)
		}
	}()

	return jobSupervisor{
		fs:          fs,
		runner:      runner,
		logger:      logger,
		dirProvider: dirProvider,

		jobFailuresServerPort: jobFailuresServerPort,

		reloadOptions: reloadOptions,
	}
}

var globalProcessLock *sync.Mutex = &sync.Mutex{}

func CheckAndSync(fs boshsys.FileSystem, monitDir string) error {
	globalProcessLock.Lock()
	defer globalProcessLock.Unlock()
	services, ok, errors := CheckJobConsistency(fs, monitDir)
	if errors != nil {
		return bosherr.New("Error checking and syncronizing jobs")
	}
	if ok == true {
		return nil
	}
	if len(services) > 0 {
		for _, servicename := range services {
			err := RemoveService(servicename)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Error removing service %s", servicename))
			}
			err = RemoveFromJobList(fs, servicename)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Error removing service %s from job list", servicename))
			}
		}
	}
	return nil
}

func (js jobSupervisor) Reload() error {
	js.logger.Debug(jobSupervisorLogTag, "Running Reload")
	js.Stop()

	js.logger.Debug(jobSupervisorLogTag, "Getting jobs")
	jobs, errs := GetJobs(js.fs, js.dirProvider.MonitJobsDir())
	if errs != nil {
		js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Errors from getting actual jobs %s", errs))
		return bosherr.WrapError(errs[0], "Error getting actual jobs")
	}
	for counter := 0; counter < len(jobs); counter++ {
		for _, service := range jobs[counter].Services {
			js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Removing service %s", service.Name))
			err := RemoveService(service.Name)
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Error removing service %s", service.Name))
			}

			preScript := service.PreStart
			js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Executing Pre Start %s", service.Name))
			stdout, stderr, exitcode, err := js.runner.RunCommand(preScript)
			js.logger.Debug("Starting Service", fmt.Sprintf("Pre-start script output for service %s : %s", service.Name, stdout))
			if err != nil || exitcode != 0 {
				return bosherr.WrapError(err, fmt.Sprintf("Exit code: %d - Error output %s", exitcode, stderr))
			}

		}

	}
	js.logger.Debug(jobSupervisorLogTag, "Starting services")
	err := js.Start()
	if err != nil {
		return bosherr.WrapError(err, "Error starting services after reload")
	}
	return nil
}

func (js jobSupervisor) Start() error {
	js.logger.Debug(jobSupervisorLogTag, "Running Start services")
	jobs, errs := GetJobs(js.fs, js.dirProvider.MonitJobsDir())
	if errs != nil {
		js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Errors from getting actual jobs %s", errs))
		return bosherr.WrapError(errs[0], "Error getting actual jobs")
	}

	for counter := 0; counter < len(jobs); counter++ {

		for _, service := range jobs[counter].Services {
			name := service.Name
			preScript := service.PreStart

			if len(preScript) > 0 {
				stdout, stderr, exitcode, err := js.runner.RunCommand(preScript)
				js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Pre-start script output for service %s : %s", name, stdout))
				if err != nil || exitcode != 0 {
					return bosherr.WrapError(err, fmt.Sprintf("Exit code: %d - Error output %s", exitcode, stderr))
				}
			}

			m, err := mgr.Connect()
			if err != nil {
				return bosherr.WrapError(err, "Connection error")
			}
			defer m.Disconnect()
			s, err := m.OpenService(name)
			if err != nil {
				return bosherr.WrapError(err, "Could not access service")
			}
			defer s.Close()
			err = s.Start(serviceArguments)
			if err != nil {
				return bosherr.WrapError(err, "Could not start service")
			}
		}
		err := js.resetStatus(jobs[counter].JobIndex, jobs[counter].JobName, "monitored")
		if err != nil {
			return bosherr.WrapError(err, "Could not reset status")
		}

	}

	return nil
}

func (js jobSupervisor) resetStatus(jobIndex int, jobName string, status string) error {
	js.logger.Debug(jobSupervisorLogTag, "Reseting status")
	targetFilename := fmt.Sprintf("%04d_%s.monitrc", jobIndex, jobName)
	targetConfigPath := filepath.Join(js.dirProvider.MonitJobsDir(), targetFilename)

	inxml, err := js.fs.ReadFile(targetConfigPath)

	if err != nil {
		return err
	}
	var jobinfo Job
	err = xml.Unmarshal(inxml, &jobinfo)

	for i, _ := range jobinfo.Services {
		jobinfo.Services[i].Status = status
	}

	outxml, err := xml.Marshal(jobinfo)
	js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Writing status %s to file %s", status, targetConfigPath))
	err = js.fs.WriteFile(targetConfigPath, outxml)
	if err != nil {
		return err
	}

	return nil
}

func (js jobSupervisor) Stop() error {
	js.logger.Debug(jobSupervisorLogTag, "Stopping services")
	jobs, errs := GetJobs(js.fs, js.dirProvider.MonitJobsDir())
	if errs != nil {
		js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Errors from getting actual jobs %s", errs))
		return bosherr.WrapError(errs[0], "Error getting actual jobs")
	}

	for counter := 0; counter < len(jobs); counter++ {

		for _, service := range jobs[counter].Services {
			name := service.Name
			preScript := service.PreStop

			if len(preScript) > 0 {
				stdout, stderr, exitcode, err := js.runner.RunCommand(preScript)
				js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Pre-stop script output for service %s : %s", name, stdout))
				if err != nil || exitcode != 0 {
					return bosherr.WrapError(err, fmt.Sprintf("Exit code: %d - Error output %s", exitcode, stderr))
				}
			}

			c := svc.Stop
			to := svc.Stopped
			m, err := mgr.Connect()
			if err != nil {
				return err
			}
			defer m.Disconnect()
			s, err := m.OpenService(name)
			if err != nil {
				return bosherr.WrapError(err, "could not access service: %v")
			}
			defer s.Close()
			status, err := s.Control(c)
			if err != nil {
				return bosherr.WrapError(err, "could not send stop: %v")
			}
			timeout := time.Now().Add(10 * time.Second)
			for status.State != to {
				if timeout.Before(time.Now()) {
					//"timeout waiting for service to go to state"
				}
				time.Sleep(300 * time.Millisecond)
				status, err = s.Query()
				if err != nil {
					return bosherr.WrapError(err, "could not retrieve service status: %v")
				}
			}
		}
	}
	return nil
}

//Desired implementation
func (js jobSupervisor) Status() (status string) {
	js.logger.Debug(jobSupervisorLogTag, "Status services")
	jobs, errs := GetJobs(js.fs, js.dirProvider.MonitJobsDir())
	if errs != nil {
		js.logger.Debug("Status Service", fmt.Sprintf("Errors from getting actual jobs %s", errs))
	}
	status = "runnning"
	for counter := 0; counter < len(jobs); counter++ {
		for _, service := range jobs[counter].Services {
			name := service.Name

			if service.Status != "monitored" {
				status = "failing"
			}

			m, err := mgr.Connect()
			if err != nil {
				status = "failing"
				return
			}
			defer m.Disconnect()
			s, err := m.OpenService(name)
			if err != nil {
				status = "unknown"
				return //"could not access service"
			}
			defer s.Close()
		}
	}
	return status
}

func (js jobSupervisor) Unmonitor() error {
	js.logger.Debug(jobSupervisorLogTag, "Unmonitor services")
	jobs, errs := GetJobs(js.fs, js.dirProvider.MonitJobsDir())
	if errs != nil {
		js.logger.Debug("Unmonitor Service", fmt.Sprintf("Errors from getting actual jobs %s", errs))
		return bosherr.WrapError(errs[0], "Error getting actual jobs")
	}

	for counter := 0; counter < len(jobs); counter++ {
		js.resetStatus(jobs[counter].JobIndex, jobs[counter].JobName, "not_monitored")
	}
	return nil
}

func (js jobSupervisor) AddJob(jobName string, jobIndex int, configPath string) error {
	js.logger.Debug(jobSupervisorLogTag, fmt.Sprintf("Adding job %s %d", jobName, jobIndex))
	targetFilename := fmt.Sprintf("%04d_%s.monitrc", jobIndex, jobName)
	targetConfigPath := filepath.Join(js.dirProvider.MonitJobsDir(), targetFilename)
	jobs_list := ReadJobs(js.fs)

	inxml, err := js.fs.ReadFile(configPath)

	if err != nil {
		return bosherr.WrapError(err, "Read from configPath in AddJob")
	}
	var jobinfo Job
	err = xml.Unmarshal(inxml, &jobinfo)

	if err != nil {
		return bosherr.WrapError(err, "Error unmarshaling file from configPath in AddJob")
	}

	jobinfo.JobName = jobName
	jobinfo.JobIndex = jobIndex
	//Add status to services -> default monitored
	for i, _ := range jobinfo.Services {
		jobinfo.Services[i].Status = "monitored"
		jobs_list.ServiceNames = append(jobs_list.ServiceNames, jobinfo.Services[i].Name)
	}

	bytes, err := xml.Marshal(jobs_list)

	if err != nil {
		return bosherr.WrapError(err, "Error marshaling to xml the added job")
	}

	err = js.fs.WriteFile(jobSupervisorPath, bytes)
	if err != nil {
		return bosherr.WrapError(err, "Error adding new job to list")
	}

	bytes, err = xml.Marshal(jobinfo)
	if err != nil {
		return bosherr.WrapError(err, "Error marshaling xml with added status for job")
	}
	err = js.fs.WriteFile(targetConfigPath, bytes)
	if err != nil {
		return bosherr.WrapError(err, "Error writing added job file")
	}

	return nil
}

func (js jobSupervisor) RemoveAllJobs() error {
	js.logger.Debug(jobSupervisorLogTag, "Removing all jobs")
	err := js.Stop()
	if err != nil {
		return bosherr.WrapError(err, "Error stoping services before removing jobs")
	}
	err = js.fs.RemoveAll(js.dirProvider.MonitJobsDir())

	if err != nil {
		return bosherr.WrapError(err, "Error removing all monitrc files")
	}

	return nil
}

//TODO implement this
func (js jobSupervisor) MonitorJobFailures(handler JobFailureHandler) (err error) {
	//alertHandler := func(smtpd.Connection, smtpd.MailAddress) (env smtpd.Envelope, err error) {
	//	//env = &alertEnvelope{
	//	//	new(smtpd.BasicEnvelope),
	//	//	handler,
	//	//	new(boshalert.MonitAlert),
	//	//}
	//	return
	//}

	//serv := &smtpd.Server{
	//	Addr:      fmt.Sprintf(":%d", js.jobFailuresServerPort),
	//	OnNewMail: alertHandler,
	//}

	//err = serv.ListenAndServe()
	//if err != nil {
	//	err = bosherr.WrapError(err, "Listen for SMTP")
	//}
	return
}