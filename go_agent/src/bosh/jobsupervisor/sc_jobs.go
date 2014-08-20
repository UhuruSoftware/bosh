package jobsupervisor

import (
	bosherr "bosh/errors"
	boshsys "bosh/system"
	"encoding/xml"
	"path/filepath"
)

type JobList struct {
	XMLName      xml.Name `xml:"JobList"`
	ServiceNames []string `xml:"ServiceName"`
}

type Job struct {
	XMLName  xml.Name  `xml:"Job"`
	JobName  string    `xml:"JobName,attr"`
	JobIndex int       `xml:"JobIndex,attr"`
	Services []Service `xml:"Service"`
}

type Service struct {
	Name     string `xml:"Name,attr"`
	PreStart string `xml:"PreStart"`
	PreStop  string `xml:"PreStop"`
	Status   string `xml:"Status"`
}

//Return full joblist stored in jobs.xml single file
func ReadJobs(fs boshsys.FileSystem) JobList {
	var jobs_list JobList
	in, err_read := fs.ReadFile(jobSupervisorPath)

	if err_read != nil {
		bosherr.WrapError(err_read, "Could not read the jobs file")
	}

	err_unmarshal := xml.Unmarshal([]byte(in), &jobs_list)

	if err_unmarshal != nil {
		bosherr.WrapError(err_unmarshal, "Could not unmarshal the jobs file")
	}

	return jobs_list
}

func GetJobs(fs boshsys.FileSystem, monitDirectory string) ([]Job, []error) {
	var job_list []Job
	var occured_errors []error

	monitFiles, err := filepath.Glob(filepath.Join(monitDirectory, "*.monitrc"))

	if err != nil {
		occured_errors = append(occured_errors, err)
	}

	for _, filePath := range monitFiles {
		var jobInfo Job
		content, err := fs.ReadFile(filePath)
		if err != nil {
			occured_errors = append(occured_errors, err)
		}
		err = xml.Unmarshal(content, &jobInfo)
		if err != nil {
			occured_errors = append(occured_errors, err)
		}
		job_list = append(job_list, jobInfo)
	}
	return job_list, occured_errors
}

//Checks if actual jobs are stored in jobs.xml file
func CheckJobConsistency(fs boshsys.FileSystem, monitDirectory string) ([]string, bool, []error) {
	stored_service_list := ReadJobs(fs)
	actual_job_list, errs := GetJobs(fs, monitDirectory)
	if errs != nil {
		return nil, false, errs
	}
	match := true

	var services_to_remove []string

	for _, serviceName := range stored_service_list.ServiceNames {
		if contains(actual_job_list, serviceName) == false {
			services_to_remove = append(services_to_remove, serviceName)
			match = false
		}
	}

	return services_to_remove, match, nil
}

func contains(job_list []Job, service_to_check string) bool {
	for _, job := range job_list {
		for _, service := range job.Services {
			if service.Name == service_to_check {
				return true
			}
		}
	}
	return false
}

func RemoveFromJobList(fs boshsys.FileSystem, name string) error {
	stored_service_list := ReadJobs(fs)
	services := removeFromServices(stored_service_list.ServiceNames, name)
	stored_service_list.ServiceNames = services

	output, err := xml.Marshal(stored_service_list)

	if err != nil {
		return err
	}

	err = fs.WriteFile(jobSupervisorPath, output)
	if err != nil {
		return err
	}

	return nil
}

func removeFromServices(services []string, name string) []string {
	var result []string
	for i, serviceName := range services {
		if serviceName == name {
			result = append(services[:i], services[(i+1):]...)
			break
		}
	}
	return result
}
