package platform

import (
	"time"

	sigar "github.com/cloudfoundry/gosigar"

	bosherror "bosh/errors"
	boshlog "bosh/logger"
	boshcdrom "bosh/platform/cdrom"
	boshudev "bosh/platform/cdrom/udevdevice"
	boshcd "bosh/platform/cdutil"
	boshcmd "bosh/platform/commands"
	boshdisk "bosh/platform/disk"
	boshnet "bosh/platform/net"
	bosharp "bosh/platform/net/arp"
	boship "bosh/platform/net/ip"
	boshstats "bosh/platform/stats"
	boshvitals "bosh/platform/vitals"
	boshdirs "bosh/settings/directories"
	boshsys "bosh/system"
)

const (
	ArpIterations          = 20
	ArpIterationDelay      = 5 * time.Second
	ArpInterfaceCheckDelay = 100 * time.Millisecond
)

const (
	SigarStatsCollectionInterval = 10 * time.Second
)

type provider struct {
	platforms map[string]Platform
}

type ProviderOptions struct {
	Linux LinuxOptions
}

func NewProvider(logger boshlog.Logger, dirProvider boshdirs.DirectoriesProvider, options ProviderOptions) (p provider) {
	runner := boshsys.NewExecCmdRunner(logger)
	fs := boshsys.NewOsFileSystem(logger)

	linuxDiskManager := boshdisk.NewLinuxDiskManager(logger, runner, fs, options.Linux.BindMountPersistentDisk)
	windowsDiskManager := boshdisk.NewWindowsDiskManager(logger, runner, fs, false)

	udev := boshudev.NewConcreteUdevDevice(runner)
	linuxCdrom := boshcdrom.NewLinuxCdrom("/dev/sr0", udev, runner)
	windowsCdrom := boshcdrom.NewWindowsCdrom(windowsDiskManager, runner, logger)

	linuxCdutil := boshcd.NewCdUtil(dirProvider.SettingsDir(), fs, linuxCdrom)
	windowsCdutil := boshcd.NewCdUtil(dirProvider.SettingsDir(), fs, windowsCdrom)

	compressor := boshcmd.NewTarballCompressor(runner, fs)
	gocompressor := boshcmd.NewGoTarballCompressor(fs)
	copier := boshcmd.NewCpCopier(runner, fs, logger)

	sigarCollector := boshstats.NewSigarStatsCollector(&sigar.ConcreteSigar{})

	// Kick of stats collection as soon as possible
	go sigarCollector.StartCollecting(SigarStatsCollectionInterval, nil)

	vitalsService := boshvitals.NewService(sigarCollector, dirProvider)

	routesSearcher := boshnet.NewCmdRoutesSearcher(runner)
	ipResolver := boship.NewIPResolver(boship.NetworkInterfaceToAddrsFunc)

	defaultNetworkResolver := boshnet.NewDefaultNetworkResolver(routesSearcher, ipResolver)
	arping := bosharp.NewArping(runner, fs, logger, ArpIterations, ArpIterationDelay, ArpInterfaceCheckDelay)

	centosNetManager := boshnet.NewCentosNetManager(fs, runner, defaultNetworkResolver, ipResolver, arping, logger)
	ubuntuNetManager := boshnet.NewUbuntuNetManager(fs, runner, defaultNetworkResolver, ipResolver, arping, logger)
	windowsNetManager := boshnet.NewWindowsNetManager(fs, runner, defaultNetworkResolver, ipResolver, arping, logger)

	centos := NewLinuxPlatform(
		fs,
		runner,
		sigarCollector,
		compressor,
		copier,
		dirProvider,
		vitalsService,
		linuxCdutil,
		linuxDiskManager,
		centosNetManager,
		500*time.Millisecond,
		options.Linux,
		logger,
	)

	ubuntu := NewLinuxPlatform(
		fs,
		runner,
		sigarCollector,
		compressor,
		copier,
		dirProvider,
		vitalsService,
		linuxCdutil,
		linuxDiskManager,
		ubuntuNetManager,
		500*time.Millisecond,
		options.Linux,
		logger,
	)

	windows := NewWindowsPlatform(
		fs,
		runner,
		sigarCollector,
		gocompressor,
		windowsCdutil,
		dirProvider,
		windowsDiskManager,
		windowsNetManager,
		500*time.Microsecond,
		logger,
	)

	p.platforms = map[string]Platform{
		"ubuntu":  ubuntu,
		"centos":  centos,
		"windows": windows,
		"dummy":   NewDummyPlatform(sigarCollector, fs, runner, dirProvider, logger),
	}
	return
}

func (p provider) Get(name string) (Platform, error) {
	plat, found := p.platforms[name]
	if !found {
		return nil, bosherror.New("Platform %s could not be found", name)
	}
	return plat, nil
}
