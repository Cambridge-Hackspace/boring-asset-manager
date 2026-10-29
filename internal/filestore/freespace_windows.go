package filestore

import "golang.org/x/sys/windows"

// freeSpace is how many bytes may still be written in dir's volume, by
// this user.
func freeSpace(dir string) (int64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, nil, nil); err != nil {
		return 0, err
	}
	return int64(free), nil
}
