package platform

// #include <windows.h>
import "C"
import (
	//"encoding/json"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unsafe"

	bosherr "bosh/errors"
	boshdpresolv "bosh/infrastructure/devicepathresolver"
	boshlog "bosh/logger"
	boshcd "bosh/platform/cdutil"
	boshcmd "bosh/platform/commands"
	boshdisk "bosh/platform/disk"
	boshnet "bosh/platform/net"
	boshstats "bosh/platform/stats"
	boshvitals "bosh/platform/vitals"
	boshsettings "bosh/settings"
	boshdir "bosh/settings/directories"
	boshdirs "bosh/settings/directories"
	boshsys "bosh/system"

	"github.com/mattn/go-ole"
	"github.com/mattn/go-ole/oleutil"
)

type windowsPlatform struct {
	fs                 boshsys.FileSystem
	cmdRunner          boshsys.CmdRunner
	collector          boshstats.StatsCollector
	compressor         boshcmd.Compressor
	copier             boshcmd.Copier
	dirProvider        boshdirs.DirectoriesProvider
	vitalsService      boshvitals.Service
	devicePathResolver boshdpresolv.DevicePathResolver
	logger             boshlog.Logger
	cdutil             boshcd.CdUtil
	diskManager        boshdisk.Manager
	netManager         boshnet.NetManager
	diskScanDuration   time.Duration
}

func NewWindowsPlatform(
	fs boshsys.FileSystem,
	cmdRunner boshsys.CmdRunner,
	collector boshstats.StatsCollector,
	compressor boshcmd.Compressor,
	cdutil boshcd.CdUtil,
	dirProvider boshdirs.DirectoriesProvider,
	diskManager boshdisk.Manager,
	netManager boshnet.NetManager,
	diskScanDuration time.Duration,
	logger boshlog.Logger,
) *windowsPlatform {
	return &windowsPlatform{
		fs:               fs,
		cmdRunner:        cmdRunner,
		collector:        collector,
		compressor:       compressor,
		copier:           boshcmd.NewCpCopier(cmdRunner, fs, logger),
		dirProvider:      dirProvider,
		vitalsService:    boshvitals.NewService(collector, dirProvider),
		cdutil:           cdutil,
		diskManager:      diskManager,
		netManager:       netManager,
		diskScanDuration: diskScanDuration,
		logger:           logger,
	}
}

func (p windowsPlatform) GetFs() (fs boshsys.FileSystem) {
	return p.fs
}

func (p windowsPlatform) GetRunner() (runner boshsys.CmdRunner) {
	return p.cmdRunner
}

func (p windowsPlatform) GetCompressor() (compressor boshcmd.Compressor) {
	return p.compressor
}

func (p windowsPlatform) GetCopier() (copier boshcmd.Copier) {
	return p.copier
}

func (p windowsPlatform) GetDirProvider() (dirProvider boshdir.DirectoriesProvider) {
	return p.dirProvider
}

func (p windowsPlatform) GetVitalsService() (service boshvitals.Service) {
	return p.vitalsService
}

func (p windowsPlatform) GetFileContentsFromCDROM(fileName string) (contents []byte, err error) {
	return p.cdutil.GetFileContents(fileName)
}

func (p windowsPlatform) GetDevicePathResolver() (devicePathResolver boshdpresolv.DevicePathResolver) {
	return p.devicePathResolver
}

func (p *windowsPlatform) SetDevicePathResolver(devicePathResolver boshdpresolv.DevicePathResolver) (err error) {
	p.devicePathResolver = devicePathResolver
	return
}

func (p windowsPlatform) SetupManualNetworking(networks boshsettings.Networks) (err error) {
	return p.netManager.SetupManualNetworking(networks, nil)
}

func (p windowsPlatform) SetupRuntimeConfiguration() (err error) {
	//_, _, _, err = p.cmdRunner.RunCommand("bosh-agent-rc")
	//if err != nil {
	//	err = bosherr.WrapError(err, "Shelling out to bosh-agent-rc")
	//}
	return
}

func (p windowsPlatform) SetupDhcp(networks boshsettings.Networks) (err error) {
	return p.netManager.SetupDhcp(networks, nil)
}

func (p windowsPlatform) CreateUser(username, password, basePath string) (err error) {

	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		p.logger.Debug("platform", "Error initilizing OLE", err)
	}

	unknown, err := oleutil.CreateObject("BoshUtilities.WindowsUsersAndGroups")
	defer unknown.Release()
	if err != nil {
		p.logger.Debug("platform", "Error creating object", err)
	}

	cons, err := unknown.QueryInterface(ole.IID_IDispatch)
	defer cons.Release()
	if err != nil {
		p.logger.Debug("platform", "Error querying interface", err)
	}

	_, err = oleutil.CallMethod(cons, "CreateUser", username, password, basePath)

	if err != nil {
		return err
	}

	return
}

func (p windowsPlatform) AddUserToGroups(username string, groups []string) (err error) {

	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		p.logger.Debug("platform", "Error initilizing OLE", err)
	}

	unknown, err := oleutil.CreateObject("BoshUtilities.WindowsUsersAndGroups")
	defer unknown.Release()
	if err != nil {
		p.logger.Debug("platform", "Error creating object", err)
	}

	cons, err := unknown.QueryInterface(ole.IID_IDispatch)
	defer cons.Release()
	if err != nil {
		p.logger.Debug("platform", "Error querying interface", err)
	}

	for _, group := range groups {

		_, err = oleutil.CallMethod(cons, "AddUserToGroup", username, group)

		if err != nil {
			return err
		}
	}

	return
}

func (p windowsPlatform) DeleteEphemeralUsersMatching(reg string) (err error) {
	compiledReg, err := regexp.Compile(reg)
	if err != nil {
		err = bosherr.WrapError(err, "Compiling regexp")
		return
	}

	matchingUsers, err := p.findEphemeralUsersMatching(compiledReg)
	if err != nil {
		err = bosherr.WrapError(err, "Finding ephemeral users")
		return
	}

	for _, user := range matchingUsers {
		p.DeleteUser(user)
	}
	return
}

func (p windowsPlatform) DeleteUser(username string) (err error) {
	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		p.logger.Debug("platform", "Error initilizing OLE", err)
	}

	unknown, err := oleutil.CreateObject("BoshUtilities.WindowsUsersAndGroups")
	defer unknown.Release()
	if err != nil {
		p.logger.Debug("platform", "Error creating object", err)
	}

	cons, err := unknown.QueryInterface(ole.IID_IDispatch)
	defer cons.Release()
	if err != nil {
		p.logger.Debug("platform", "Error querying interface", err)
	}

	_, err = oleutil.CallMethod(cons, "DeleteUser", username)

	if err != nil {
		return err
	}

	return
}

func (p windowsPlatform) findEphemeralUsersMatching(reg *regexp.Regexp) (matchingUsers []string, err error) {
	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		p.logger.Debug("platform", "Error initilizing OLE", err)
	}

	unknown1, errr := oleutil.CreateObject("BoshUtilities.WindowsUsersAndGroups")
	if errr != nil {
		p.logger.Debug("platform", "Error creating object", errr)
	}

	cons1, errr := unknown1.QueryInterface(ole.IID_IDispatch)
	if errr != nil {
		p.logger.Debug("platform", "Error querying interface", errr)
	}

	result, err := oleutil.CallMethod(cons1, "GetUsers")
	olearray := result.ToArray()
	userlist := olearray.ToStringArray()

	for _, user := range userlist {
		matchesReg := reg.MatchString(user)
		matchesPrefix := strings.HasPrefix(user, boshsettings.EphemeralUserPrefix)
		if matchesPrefix && matchesReg {
			matchingUsers = append(matchingUsers, user)
		}
	}
	return
}

//TO DO: Set up ssh with assumption of cygwin installation in the baseDir
func (p windowsPlatform) SetupSsh(publicKey, username string) (err error) {
	baseDir := filepath.Join(p.dirProvider.BaseDir(), "cygwin")

	//cannot be tested with fake filesystem
	//if _, err := os.Stat(baseDir); err != nil {
	//	if os.IsNotExist(err) {
	//		err = bosherr.WrapError(err, "Finding cygwin dir in baseDir from dirProvider")
	//		return err
	//	}
	//}

	sshPath := filepath.Join(baseDir, "admin_home", ".ssh")
	p.fs.MkdirAll(sshPath, os.FileMode(0700))

	authKeysPath := filepath.Join(sshPath, "authorized_keys")
	err = p.fs.WriteFileString(authKeysPath, publicKey)
	if err != nil {
		err = bosherr.WrapError(err, "Creating authorized_keys file")
		return
	}

	return
}

//windows uses non-encrypted passwords
func (p windowsPlatform) SetUserPassword(user, encryptedPwd string) (err error) {
	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		fmt.Println(err)
	}

	unknown, err := oleutil.CreateObject("BoshUtilities.WindowsUsersAndGroups")
	defer unknown.Release()
	if err != nil {
		fmt.Println(err)
	}

	cons, err := unknown.QueryInterface(ole.IID_IDispatch)
	defer cons.Release()
	if err != nil {
		fmt.Println(err)
	}

	_, err = oleutil.CallMethod(cons, "SetUserPassword", user, encryptedPwd)

	if err != nil {
		return err
	}

	return
}

func (p windowsPlatform) SetupHostname(hostname string) (err error) {
	old_hostname, _, _, err := p.cmdRunner.RunCommand("hostname")
	if err != nil {
		err = bosherr.WrapError(err, "Shelling out to hostname")
		return
	}
	if old_hostname == hostname {
		return
	}
	_, _, _, err = p.cmdRunner.RunCommand("netdom", "RENAMECOMPUTER", strings.TrimSpace(old_hostname), "/NewName", hostname, "/force")

	if err != nil {
		err = bosherr.WrapError(err, "Shelling out to netdom")
		return
	}

	return
}

//TO DO: Assumption that LogRotate is installed in baseDir

func (p windowsPlatform) SetupLogrotate(groupName, basePath, size string) (err error) {
	WinlogRotatorPath := filepath.Join(p.dirProvider.BaseDir(), "LogRotator")

	buffer := bytes.NewBuffer([]byte{})
	t := template.Must(template.New("logrotate-d-config").Parse(logRotateWindowsTemplate))

	type logrotateArgs struct {
		BasePath string
		Size     string
	}

	err = t.Execute(buffer, logrotateArgs{basePath, size})
	if err != nil {
		err = bosherr.WrapError(err, "Generating logrotate config")
		return
	}

	err = p.fs.WriteFile(filepath.Join(WinlogRotatorPath, "LogRotator.xml"), buffer.Bytes())
	if err != nil {
		err = bosherr.WrapError(err, "Writing to LogRotator.xml")
		return
	}

	//Restart logrotator service to reload configuration changes
	_, _, _, err = p.GetRunner().RunCommand("net", "stop", "logrotator")
	if err != nil {
		fmt.Println("Failed stopping logrotator")
	}
	_, _, _, err = p.GetRunner().RunCommand("net", "start", "logrotator")
	if err != nil {
		fmt.Println("Failed starting logrotator")
	}

	return
}

const logRotateWindowsTemplate = `<?xml version="1.0" encoding="utf-8" ?>
<logRotator poolInterval="900000">  
  <pattern action="rotate" dirPath="{{ .BasePath }}\data\sys\log\*.log" filePattern="*.log" offset="00:01:00" subDirs="true" size="{{.Size}}"/>
  <pattern action="delete" dirPath="{{ .BasePath }}\data\sys\log\*.log" filePattern="*.gz" offset="00:10:00" deleteUnCompressed="false" subDirs="true"/>
</logRotator>`

func (p windowsPlatform) SetTimeWithNtpServers(servers []string) (err error) {
	if len(servers) == 0 {
		return
	}

	err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()

	if err != nil {
		p.logger.Debug("platform", "Error initilizing OLE", err)
	}

	unknown, errr := oleutil.CreateObject("BoshUtilities.NtpClient")
	if errr != nil {
		p.logger.Debug("platform", "Error creating object", errr)
	}

	cons, errr := unknown.QueryInterface(ole.IID_IDispatch)
	if errr != nil {
		p.logger.Debug("platform", "Error querying interface", errr)
	}

	for _, server := range servers {
		_, err := oleutil.CallMethod(cons, "Init", server)
		if err != nil {
			return err
		}

		//Upon receiving true argument the Connect sets the correct time for the machine
		//Must be run as administrator to be allowed to make the change
		_, errconnect := oleutil.CallMethod(cons, "Connect", true)
		if errconnect == nil {
			return nil
		}
	}

	return
}

//realPath should be disk id
func (p windowsPlatform) SetupEphemeralDiskWithPath(realPath string) (err error) {

	swapMountPoint := filepath.Join(p.dirProvider.BaseDir(), "swap")
	if realPath == "" {
		p.logger.Debug("platform", "Using root disk as ephemeral disk")
		return nil
	}

	swapSize, windowsSize, err := p.calculateEphemeralDiskPartitionSizes(realPath)
	if err != nil {
		return bosherr.WrapError(err, "Calculating partition sizes")
	}

	err = p.createAndMount(realPath, p.dirProvider.DataDir(), windowsSize)
	if err != nil {
		return bosherr.WrapError(err, "Creating ephemeral disk")
	}

	err = p.createAndMount(realPath, swapMountPoint, swapSize)
	if err != nil {
		return bosherr.WrapError(err, "Creating swap disk")
	}
	err = SetPageFile(swapMountPoint, 2, uint(swapSize))
	if err != nil {
		p.logger.Error("platform", "setting page file", err)
		return bosherr.WrapError(err, "Error setting page file to swap partition")
	}

	return nil
}

func (p windowsPlatform) createAndMount(diskId string, mountPath string, sizeInMB uint64) (err error) {
	p.logger.Debug("platform", "Creating and mounting partition for", mountPath)
	err = p.fs.MkdirAll(mountPath, os.FileMode(0750))
	if err != nil {
		return bosherr.WrapError(err, "Creating data dir")
	}

	partitions := []boshdisk.Partition{
		{SizeInMb: sizeInMB, Type: boshdisk.PartitionTypeWindows},
	}
	err = p.diskManager.GetPartitioner().Partition(diskId, partitions)
	if err != nil {
		return bosherr.WrapError(err, "Partitioning disk with id: %d", diskId)
	}

	//map of volumes with volume number as key and details as string
	volumes, err := boshdisk.DiskPart{}.GetVolumes("Partition")
	if err != nil {
		return bosherr.WrapError(err, "retrieving volumes information")
	}

	//format all raw volumes
	for index, details := range volumes {
		p.logger.Debug("platform", fmt.Sprintf("Formating volume with index %d and details %s", index, details))
		if strings.Contains(details, "RAW") {
			volumeNumber := strconv.Itoa(index)
			err = p.diskManager.GetFormatter().Format(volumeNumber, boshdisk.FileSystemNtfs)
			if err != nil {
				return bosherr.WrapError(err, "Formatting volume %s", volumeNumber)
			}
			err = p.diskManager.GetMounter().Mount(volumeNumber, mountPath)
			if err != nil {
				return bosherr.WrapError(err, "Mounting volume %d to %s", volumeNumber, mountPath)
			}
			break
		}
	}
	return

}

func (p windowsPlatform) SetupDataDir() error {
	dataDir := p.dirProvider.DataDir()

	sysDir := filepath.Join(dataDir, "sys")

	logDir := filepath.Join(sysDir, "log")
	err := p.fs.MkdirAll(logDir, os.FileMode(0750))
	if err != nil {
		return bosherr.WrapError(err, "Making %s dir", logDir)
	}

	_, _, _, err = p.cmdRunner.RunCommand("takeown", "/F", sysDir, "/A", "/R")
	if err != nil {
		return bosherr.WrapError(err, "takeown %s", sysDir)
	}

	_, _, _, err = p.cmdRunner.RunCommand("takeown", "/F", logDir, "/R")
	if err != nil {
		return bosherr.WrapError(err, "takeown %s", logDir)
	}

	runDir := filepath.Join(sysDir, "run")
	err = p.fs.MkdirAll(runDir, os.FileMode(0750))
	if err != nil {
		return bosherr.WrapError(err, "Making %s dir", runDir)
	}

	_, _, _, err = p.cmdRunner.RunCommand("takeown", "/F", runDir, "/A", "/R")
	if err != nil {
		return bosherr.WrapError(err, "takeown %s", runDir)
	}

	return nil
}

func (p windowsPlatform) SetupTmpDir() error {
	systemTmpDir := "C:\\Windows\\Temp"
	boshTmpDir := p.dirProvider.TmpDir()
	//boshRootTmpPath := filepath.Join(p.dirProvider.DataDir(), "root_tmp")

	// 0755 to make sure that vcap user can use new temp dir
	err := p.fs.MkdirAll(boshTmpDir, os.FileMode(0755))
	if err != nil {
		return bosherr.WrapError(err, "Creating temp dir")
	}

	err = os.Setenv("TMPDIR", boshTmpDir)
	if err != nil {
		return bosherr.WrapError(err, "Setting TMPDIR")
	}

	err = p.changeTmpDirPermissions(systemTmpDir)
	if err != nil {
		return err
	}

	return nil
}

func (p windowsPlatform) changeTmpDirPermissions(path string) error {
	_, _, _, err := p.cmdRunner.RunCommand("takeown", "/F", path, "/A", "/R")
	if err != nil {
		return bosherr.WrapError(err, "takeown %s", path)
	}

	_, _, _, err = p.cmdRunner.RunCommand("icacls", path, "/grant", "Everyone:F", "/t")
	if err != nil {
		return bosherr.WrapError(err, "icacls %s", path)
	}
	return nil
}

//devicePath needs to represent the volume id
func (p windowsPlatform) MountPersistentDisk(devicePath, mountPoint string) (err error) {
	p.logger.Debug("platform", "Mounting persistent disk volume %s at %s", devicePath, mountPoint)

	diskSize, errDiskSize := p.diskManager.GetPartitioner().GetDeviceSizeInMb(devicePath)
	if errDiskSize != nil {
		return bosherr.WrapError(errDiskSize, "Retrieving persistent disk size")
	}

	err = p.createAndMount(devicePath, mountPoint, diskSize)
	if err != nil {
		return bosherr.WrapError(err, "Creating persistent disk")
	}
	return
}

//devicePath must be volume id
func (p windowsPlatform) UnmountPersistentDisk(devicePath string) (didUnmount bool, err error) {

	//p.logger.Debug("platform", "Unmounting persistent disk %s", devicePath)

	_, err = strconv.Atoi(devicePath)

	if err != nil {
		return false, bosherr.WrapError(err, "Unmount persistent disk, incorrect volume id")
	}

	return p.diskManager.GetMounter().Unmount(devicePath)
}

func (p windowsPlatform) NormalizeDiskPath(attachment string) (devicePath string, found bool) {
	return attachment, true
}

func (p windowsPlatform) MigratePersistentDisk(fromMountPoint, toMountPoint string) (err error) {

	result, err := p.diskManager.GetMounter().IsMountPoint(toMountPoint)

	if result == false {
		err = bosherr.WrapError(err, "Destination is not a valid mount point")
		return
	}

	result, err = p.diskManager.GetMounter().IsMountPoint(fromMountPoint)

	if result == false {
		err = bosherr.WrapError(err, "Source is not a valid mount point")
		return
	}

	copycommand := fmt.Sprintf("robocopy %s %s *.* /COPYALL /e", fromMountPoint, toMountPoint)

	_, _, _, err = p.cmdRunner.RunCommand(copycommand)
	if err != nil {
		err = bosherr.WrapError(err, "Copying files from old disk to new disk")
		return
	}

	err = p.diskManager.GetMounter().Remount(toMountPoint, fromMountPoint)
	if err != nil {
		err = bosherr.WrapError(err, "Remounting new disk on original mountpoint")
	}
	return
}

func (p windowsPlatform) IsMountPoint(path string) (result bool, err error) {
	return p.diskManager.GetMounter().IsMountPoint(path)
}

//path should be volume ID
func (p windowsPlatform) IsPersistentDiskMounted(path string) (result bool, err error) {
	return p.diskManager.GetMounter().IsMounted(path)
}

func (p windowsPlatform) StartMonit() (err error) {
	//not needed on the windowsPlatform
	return nil
}

func (p windowsPlatform) SetupMonitUser() (err error) {
	//not needed on the windowsPlatform
	return
}

func (p windowsPlatform) GetMonitCredentials() (username, password string, err error) {
	//not needed on the windowsPlatform
	return
}

func (p windowsPlatform) PrepareForNetworkingChange() error {
	//not needed on the windowsPlatform
	return nil
}

func (p windowsPlatform) GetDefaultNetwork() (boshsettings.Network, error) {
	return p.netManager.GetDefaultNetwork()
	/*var network boshsettings.Network

	networkPath := filepath.Join(p.dirProvider.BoshDir(), "windows-default-network-settings.json")
	contents, err := p.fs.ReadFile(networkPath)
	if err != nil {
		return network, nil
	}

	err = json.Unmarshal([]byte(contents), &network)
	if err != nil {
		return network, bosherr.WrapError(err, "Unmarshal json settings")
	}

	return network, nil*/
}

func (p windowsPlatform) calculateEphemeralDiskPartitionSizes(devicePath string) (swapSize, windowsSize uint64, err error) {
	memStats, err := p.collector.GetMemStats()
	if err != nil {
		err = bosherr.WrapError(err, "Getting mem stats")
		return
	}

	totalMemInMb := memStats.Total / uint64(1024*1024)

	diskSizeInMb, err := p.diskManager.GetPartitioner().GetDeviceSizeInMb(devicePath)
	if err != nil {
		err = bosherr.WrapError(err, "Getting device size")
		return
	}

	if totalMemInMb > diskSizeInMb/2 {
		swapSize = diskSizeInMb / 2
	} else {
		swapSize = totalMemInMb
	}

	windowsSize = diskSizeInMb - swapSize

	return
}

func enableTokenPrivilege() {
	var tokenH C.HANDLE
	defer C.CloseHandle(tokenH)
	var tkp C.TOKEN_PRIVILEGES
	succeedOpenToken := C.OpenProcessToken(C.GetCurrentProcess(), C.TOKEN_ADJUST_PRIVILEGES|C.TOKEN_QUERY, &tokenH)

	if succeedOpenToken == C.FALSE {
		lastError := C.GetLastError()
		panic(fmt.Sprintf("OpenProcess failed with error: %d", int(lastError)))
	}

	seDebugPrivilege := (*C.CHAR)(unsafe.Pointer(C.CString("SeCreatePagefilePrivilege")))
	succedLookupPrivilege := C.LookupPrivilegeValue(nil, seDebugPrivilege, &tkp.Privileges[0].Luid)

	if succedLookupPrivilege == C.FALSE {
		lastError := C.GetLastError()
		panic(fmt.Sprintf("LookupPrivilegeValue failed with error: %d", int(lastError)))
	}
	tkp.PrivilegeCount = 1
	tkp.Privileges[0].Attributes = C.SE_PRIVILEGE_ENABLED

	succeedAdjustToken := C.AdjustTokenPrivileges(tokenH, C.FALSE, &tkp, C.DWORD(0), nil, nil)
	if succeedAdjustToken == C.FALSE {
		lastError := C.GetLastError()
		panic(fmt.Sprintf("AdjustTokenPrivileges failed with error: %d", int(lastError)))
	}
}

//used for setting PageFile, initial size must be grater than 1
func SetPageFile(path string, initialSizeMB uint, maximumSizeMB uint) error {
	newPath := filepath.Join(path, "pagefile.sys")

	enableTokenPrivilege()

	ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, _ := oleutil.CreateObject("WbemScripting.SWbemLocator")
	defer unknown.Release()

	wmi, _ := unknown.QueryInterface(ole.IID_IDispatch)
	defer wmi.Release()

	serviceRaw, _ := oleutil.CallMethod(wmi, "ConnectServer")
	service := serviceRaw.ToIDispatch()
	defer service.Release()

	resultRaw, _ := oleutil.CallMethod(service, "ExecQuery", "SELECT * FROM Win32_PageFileSetting")
	result := resultRaw.ToIDispatch()
	defer result.Release()

	countVar, _ := oleutil.GetProperty(result, "Count")
	count := int(countVar.Val)

	for i := 0; i < count; i++ {
		itemRaw, _ := oleutil.CallMethod(result, "ItemIndex", i)
		item := itemRaw.ToIDispatch()
		defer item.Release()

		_, err := oleutil.PutProperty(item, "Name", newPath)
		if err != nil {
			return fmt.Errorf("Error setting name", err)
		}
		res, erro := oleutil.CallMethod(item, "Put_")
		_, err = oleutil.PutProperty(item, "InitialSize", initialSizeMB)
		if err != nil {
			return fmt.Errorf("Error setting initial size", err)
		}
		_, err = oleutil.PutProperty(item, "MaximumSize", maximumSizeMB)
		if err != nil {
			return fmt.Errorf("Error setting maximum size", err)
		}
		//commit changes
		res, erro = oleutil.CallMethod(item, "Put_")
		if erro != nil {
			return fmt.Errorf("Error commiting changes", erro, res)
		}
	}

	return nil
}
