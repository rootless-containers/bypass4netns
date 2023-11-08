package util

import (
	"fmt"
	"os"
)

// shrinkID shrinks id to short(12 chars) id
// 6d9bcda7cebd551ddc9e3173d2139386e21b56b241f8459c950ef58e036f6bd8
// to
// 6d9bcda7cebd
func ShrinkID(id string) string {
	if len(id) < 12 {
		return id
	}

	return id[0:12]
}

func SameUserNS(pidX, pidY int) (bool, error) {
	nsX := fmt.Sprintf("/proc/%d/ns/user", pidX)
	nsY := fmt.Sprintf("/proc/%d/ns/user", pidY)
	nsXResolved, err := os.Readlink(nsX)
	if err != nil {
		return false, err
	}
	nsYResolved, err := os.Readlink(nsY)
	if err != nil {
		return false, err
	}
	return nsXResolved == nsYResolved, nil
}
