package internal

import (
	"fmt"
	"os"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseutil"

	"os/signal"

	"sync"
	"syscall"
	"time"

	"golang.org/x/net/context"

	daemon "github.com/sevlyar/go-daemon"
)

var waitedForSignal os.Signal

func waitForSignal(wg *sync.WaitGroup) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGUSR1, syscall.SIGUSR2)

	wg.Add(1)
	go func() {
		waitedForSignal = <-signalChan
		wg.Done()
	}()
}

func kill(pid int, s os.Signal) (err error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	defer p.Release()

	err = p.Signal(s)
	if err != nil {
		return err
	}
	return
}

func Mount(ctx context.Context, mountPoint string, gen3FuseConfig *Gen3FuseConfig, manifestURL string) (fs *Gen3Fuse, mfs *fuse.MountedFileSystem, err error) {
	fs, err = NewGen3Fuse(ctx, gen3FuseConfig, manifestURL)
	if err != nil {
		return
	}

	if fs == nil {
		err = fmt.Errorf("Mount: initialization failed")
		return
	}
	server := fuseutil.NewFileSystemServer(fs)

	// Mount the file system.
	mountCfg := &fuse.MountConfig{
		FSName:                  "gen3fuse",
		ErrorLogger:             nil,
		DisableWritebackCaching: true,
		ReadOnly:                true,
		Options:                 map[string]string{},
	}
	mountCfg.Options["allow_other"] = ""

	mfs, err = fuse.Mount(mountPoint, server, mountCfg)
	if err != nil {
		return
	}

	if mfs == nil {
		err = fmt.Errorf("Mount: %v", err)
		return
	}

	FuseLog(fmt.Sprintf("Your exported files have been mounted to %s/\n", mountPoint))

	return
}

func Unmount(mountPoint string) (err error) {
	err = fuse.Unmount(mountPoint)
	return err
}

func InitializeApp(gen3FuseConfig *Gen3FuseConfig, manifestURL string, mountPoint string) {
	var child *os.Process

	f := func() (err error) {
		defer func() {
			time.Sleep(time.Second)
		}()

		var wg sync.WaitGroup
		waitForSignal(&wg)

		daemonCtx := daemon.Context{LogFileName: "/dev/stdout"}
		child, err = daemonCtx.Reborn()

		if err != nil {
			panic(fmt.Sprintf("unable to daemonize: %v", err))
		}

		if child != nil {
			// attempt to wait for child to notify parent
			wg.Wait()
			if waitedForSignal == syscall.SIGUSR1 {
				return
			}
			return fuse.EINVAL
		}
		// kill our own waiting goroutine
		kill(os.Getpid(), syscall.SIGUSR1)
		wg.Wait()
		defer daemonCtx.Release()

		// Mount the file system.
		var mfs *fuse.MountedFileSystem
		ctx := context.Background()

		_, mfs, err = Mount(ctx, mountPoint, gen3FuseConfig, manifestURL)

		if err != nil {
			kill(os.Getppid(), syscall.SIGUSR2)
			FuseLog("Mounting file system: " + err.Error())
		} else {
			kill(os.Getppid(), syscall.SIGUSR1)

			// Wait for the file system to be unmounted.
			err = mfs.Join(context.Background())
			if err != nil {
				err = fmt.Errorf("MountedFileSystem.Join: %v", err)
				return
			}
		}
		return
	}

	err := f()

	if err != nil {
		FuseLog(err.Error())
		fmt.Println("Unable to mount file system: " + err.Error() + "\n See " + gen3FuseConfig.LogFilePath + " for more details. ")
		os.Exit(1)
	}
}
