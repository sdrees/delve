// +build linux,amd64 linux,arm64

package native

import (
	"syscall"
	"unsafe"

	sys "golang.org/x/sys/unix"
)

// ProcessVmRead calls process_vm_readv
func ProcessVmRead(tid int, addr uintptr, data []byte) (int, error) {
	len_iov := uint64(len(data))
	local_iov := sys.Iovec{Base: &data[0], Len: len_iov}
	remote_iov := sys.Iovec{Base: (*byte)(unsafe.Pointer(addr)), Len: len_iov}
	p_local := uintptr(unsafe.Pointer(&local_iov))
	p_remote := uintptr(unsafe.Pointer(&remote_iov))
	n, _, err := syscall.Syscall6(sys.SYS_PROCESS_VM_READV, uintptr(tid), p_local, 1, p_remote, 1, 0)
	if err != syscall.Errno(0) {
		return 0, err
	}
	return int(n), nil
}

// ProcessVmWrite calls process_vm_writev
func ProcessVmWrite(tid int, addr uintptr, data []byte) (int, error) {
	len_iov := uint64(len(data))
	local_iov := sys.Iovec{Base: &data[0], Len: len_iov}
	remote_iov := sys.Iovec{Base: (*byte)(unsafe.Pointer(addr)), Len: len_iov}
	p_local := uintptr(unsafe.Pointer(&local_iov))
	p_remote := uintptr(unsafe.Pointer(&remote_iov))
	n, _, err := syscall.Syscall6(sys.SYS_PROCESS_VM_WRITEV, uintptr(tid), p_local, 1, p_remote, 1, 0)
	if err != syscall.Errno(0) {
		return 0, err
	}
	return int(n), nil
}
