package jobsupervisor

import (
	"code.google.com/p/winsvc/debug"
	"code.google.com/p/winsvc/eventlog"
	"code.google.com/p/winsvc/mgr"
	"code.google.com/p/winsvc/svc"
	"fmt"
	"path/filepath"
)

var elog debug.Log

type myservice struct{}

func (m *myservice) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	return
}

func exePath(configPath string) ([]string, error) {
	executables, err := filepath.Glob(filepath.Join(configPath, "*.exe"))
	if err != nil {
		return nil, err
	}
	return executables, nil
}

func RemoveService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", name)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		return err
	}
	err = eventlog.Remove(name)
	if err != nil {
		return fmt.Errorf("RemoveEventLogSource() failed: %s", err)
	}
	return nil
}

func InstallService(name, configPath string) error {
	executables, err := exePath(configPath)
	for _, exepath := range executables {
		fmt.Println(exepath)
		if err != nil {
			return err
		}
		m, err := mgr.Connect()
		if err != nil {
			return err
		}
		defer m.Disconnect()
		s, err := m.OpenService(name)
		if err == nil {
			s.Close()
			return fmt.Errorf("service %s already exists", name)
		}
		s, err = m.CreateService(name, exepath, mgr.Config{DisplayName: name})
		if err != nil {
			return err
		}
		defer s.Close()
		err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
		if err != nil {
			fmt.Println(err)
			s.Delete()
			return fmt.Errorf("SetupEventLogSource() failed: %s", err)
		}
	}
	return nil
}
