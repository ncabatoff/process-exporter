package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"runtime"
	"syscall"
	"unsafe"
)

var ready = make(chan struct{})

func init() {
	var (
		flagWaiting        = flag.Int("waiting", 1, "minimum number of waiting threads")
		flagUserBusy       = flag.Int("userbusy", 1, "minimum number of userbusy threads")
		flagSysBusy        = flag.Int("sysbusy", 1, "minimum number of sysbusy threads")
		flagBlocking       = flag.Int("blocking", 1, "minimum number of io blocking threads")
		flagWriteSizeBytes = flag.Int("write-size-bytes", 1024*1024, "how many bytes to write each cycle")
	)
	flag.Parse()
	runtime.LockOSThread()
	for i := 0; i < *flagWaiting; i++ {
		go waiting()
		<-ready
	}
	for i := 0; i < *flagUserBusy; i++ {
		go userbusy()
		<-ready
	}
	for i := 0; i < *flagSysBusy; i++ {
		go diskio(false, *flagWriteSizeBytes)
		<-ready
	}
	for i := 0; i < *flagBlocking; i++ {
		go diskio(true, *flagWriteSizeBytes)
		<-ready
	}
}

func main() {
	c := make(chan struct{})
	fmt.Println("ready")
	<-c
}

func setPrName(name string) error {
	bytes := append([]byte(name), 0)
	ptr := unsafe.Pointer(&bytes[0])

	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, syscall.PR_SET_NAME, uintptr(ptr), 0, 0, 0, 0)
	if errno != 0 {
		return syscall.Errno(errno)
	}
	return nil
}

func waiting() {
	runtime.LockOSThread()
	setPrName("waiting")
	ready <- struct{}{}

	c := make(chan struct{})
	<-c
}

func userbusy() {
	runtime.LockOSThread()
	setPrName("userbusy")
	ready <- struct{}{}

	i := 1.0000001
	for {
		i *= i
	}
}

func diskio(sync bool, writesize int) {
	runtime.LockOSThread()
	if sync {
		setPrName("blocking")
	} else {
		setPrName("sysbusy")
	}

	// Use random data because if we're on a filesystem that does compression like ZFS,
	// using zeroes is almost a no-op.
	b := make([]byte, writesize)
	_, err := rand.Read(b)
	if err != nil {
		panic("unable to get rands: " + err.Error())
	}

	f, err := ioutil.TempFile("", "loadgen")
	if err != nil {
		panic("unable to create tempfile: " + err.Error())
	}
	defer f.Close()

	sentready := false

	offset := int64(0)
	for {
		_, err = f.WriteAt(b, offset)
		if err != nil {
			panic("unable to write tempfile: " + err.Error())
		}

		if sync {
			err = f.Sync()
			if err != nil {
				panic("unable to sync tempfile: " + err.Error())
			}
		}

		_, err = f.ReadAt(b, 0)
		if err != nil {
			panic("unable to read tempfile: " + err.Error())
		}
		if !sentready {
			ready <- struct{}{}
			sentready = true
		}
		offset++
	}
}
