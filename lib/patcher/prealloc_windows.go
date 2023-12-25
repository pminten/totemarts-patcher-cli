//go:build windows

package patcher

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                        = windows.NewLazySystemDLL("ntdll.dll")
	NtQueryVolumeInformationFile = ntdll.NewProc("NtQueryVolumeInformationFile")
)

const (
	fileFsSizeInformation = 3 // https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ne-wdm-_fsinfoclass
	fileAllocationInfo    = 5 // https://learn.microsoft.com/en-us/windows/win32/api/minwinbase/ne-minwinbase-file_info_by_handle_class
)

// https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntddk/ns-ntddk-_file_fs_size_informations
type FILE_FS_SIZE_INFORMATION struct {
	TotalAllocationUnits     uint64
	AvailableAllocationUnits uint64
	SectorsPerAllocationUnit uint32
	BytesPerSector           uint32
}

// https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_file_allocation_information
type FILE_FS_ALLOCATION_INFORMATION struct {
	AllocationSize uint64
}

func getFileAllocationUnitSize(handle windows.Handle) (uint64, error) {
	var iosb windows.IO_STATUS_BLOCK
	var fsSizeInfo FILE_FS_SIZE_INFORMATION
	// https: //learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/nf-ntifs-ntqueryvolumeinformationfile
	_, _, err := NtQueryVolumeInformationFile.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&iosb)),
		uintptr(unsafe.Pointer(&fsSizeInfo)),
		unsafe.Sizeof(fsSizeInfo),
		fileFsSizeInformation)
	if err != nil && err != syscall.Errno(0) {
		return 0, fmt.Errorf("NtQueryVolumeInformation failed: %w", err)
	}
	return uint64(fsSizeInfo.SectorsPerAllocationUnit) * uint64(fsSizeInfo.BytesPerSector), nil
}

func setFileAllocationSize(handle windows.Handle, size uint64) error {
	fsAllocInfo := FILE_FS_ALLOCATION_INFORMATION{
		AllocationSize: size,
	}
	// https://learn.microsoft.com/nl-nl/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle
	err := windows.SetFileInformationByHandle(
		handle,
		windows.FileAllocationInfo,
		(*byte)(unsafe.Pointer(&fsAllocInfo)),
		uint32(unsafe.Sizeof(fsAllocInfo)),
	)
	if err != nil {
		return fmt.Errorf("SetFileInformationByHandle failed: %w", err)
	}
	return nil
}

// CreateWithSizeHint creates a file, reserving space to grow it to size.
func CreateWithSizeHint(filename string, size int64) (file *os.File, err error) {
	// Scribbled a lot from https://github.com/rclone/rclone/issues/2469
	// and https://devblogs.microsoft.com/oldnewthing/20160714-00/?p=93875
	if size < 0 {
		return nil, fmt.Errorf("invalid preallocate size %d", size)
	}
	usize := uint64(size)
	var nullHandle windows.Handle
	filename16 := utf16.Encode([]rune(filename + "\x00"))
	handle, err := windows.CreateFile(&filename16[0], windows.GENERIC_WRITE, windows.FILE_SHARE_READ,
		nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, nullHandle)
	if err != nil {
		return nil, fmt.Errorf("preallocation CreateFile failed for '%s': %w", filename, err)
	}

	defer func() {
		// Only act if there's an error as we need to return that handle.
		if err != nil {
			// Already reporting an error which is likely a better hint for what's going wrong.
			// Don't overwrite that by changing err.
			if closeErr := windows.CloseHandle(handle); closeErr != nil {
				log.Printf("preallocation CloseHandle failed for '%s': %s", filename, err)
			}
		}
	}()

	// I'm not entirely sure the allocation unit must be a multiple of the cluster size,
	// read some conflicting informatin, but it's in any case common and probably a good idea.
	allocUnitSize, err := getFileAllocationUnitSize(handle)
	if err != nil {
		return nil, fmt.Errorf("preallocation file allocation unit determination failed for '%s': %w", filename, err)
	}
	// There are smarter ways to write this, but this is easier to mentally verify.
	var units uint64
	if usize%allocUnitSize == 0 {
		units = (usize / allocUnitSize) * allocUnitSize
	} else {
		// This doesn't even hurt all that much as the allocation size will snap back to what's actually
		// written once the file is closed.
		units = (1 + (usize / allocUnitSize)) * allocUnitSize
	}

	if err := setFileAllocationSize(handle, units); err != nil {
		return nil, fmt.Errorf("preallocation setting file allocation failed for '%s': %w", filename, err)
	}

	return os.NewFile(uintptr(handle), filename), nil
}
