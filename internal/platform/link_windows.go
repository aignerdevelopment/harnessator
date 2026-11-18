// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build windows

package platform

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows materialization uses NTFS junctions for directories (no admin or
// developer-mode required, unlike symlinks) and copies for single files,
// per the day-one multi-OS mandate. File
// copies are drift-checked by harnessator verify digests, keeping the
// "never silently diverge" promise even where live linking isn't used.
//
// Integration testing requires a real Windows machine; the REPARSE
// encoding below follows
// MS-FSCC REPARSE_DATA_BUFFER for mount points exactly.

func link(src, dst string, dir bool) error {
	if dir {
		return junction(src, dst)
	}
	return copyFile(src, dst)
}

// junction creates dst as an NTFS junction (reparse point) to abs(src).
func junction(src, dst string) error {
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, 0o777); err != nil {
		return fmt.Errorf("junction dir %s: %w", dst, err)
	}
	p, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	// The documented recipe for setting reparse points opens the fresh
	// directory with DELETE access and reparse-point flags.
	h, err := windows.CreateFile(
		p,
		windows.DELETE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0)
	if err != nil {
		return fmt.Errorf("open %s for junction: %w", dst, err)
	}
	defer windows.CloseHandle(h)

	buf := mountPointReparse(abs)
	var ret uint32
	if err := windows.DeviceIoControl(h,
		windows.FSCTL_SET_REPARSE_POINT,
		&buf[0], uint32(len(buf)), nil, 0, &ret, nil); err != nil {
		return fmt.Errorf("set junction %s -> %s: %w", dst, abs, err)
	}
	return nil
}

// mountPointReparse encodes a mount-point REPARSE_DATA_BUFFER (MS-FSCC):
//
//	[0]  uint32 ReparseTag = IO_REPARSE_TAG_MOUNT_POINT (0xA0000003)
//	[4]  uint16 ReparseDataLength  = 16 + len(PathBuffer)
//	[6]  uint16 Reserved
//	[8]  uint32 SubstituteNameOffset = 0
//	[12] uint32 SubstituteNameLength = bytes of sub name, excluding NUL
//	[16] uint32 PrintNameOffset      = bytes of sub name, including NUL
//	[20] uint32 PrintNameLength      = bytes of print name, excluding NUL
//	[24] PathBuffer: "\\??\<abs>" NUL  <abs> NUL  (UTF-16LE)
func mountPointReparse(target string) []byte {
	sub := `\??\` + filepath.Clean(target) // canonical device prefix, no trailing issues
	printName := filepath.Clean(target)

	subU, _ := windows.UTF16FromString(sub) // includes NUL
	printU, _ := windows.UTF16FromString(printName)

	pathLen := 2*len(subU) + 2*len(printU)
	dataLen := 16 + pathLen
	buf := make([]byte, 8+dataLen)

	binary.LittleEndian.PutUint32(buf[0:], 0xA0000003)
	binary.LittleEndian.PutUint16(buf[4:], uint16(dataLen))
	binary.LittleEndian.PutUint16(buf[6:], 0)
	binary.LittleEndian.PutUint32(buf[8:], 0)
	binary.LittleEndian.PutUint32(buf[12:], uint32(2*len(subU)-2))
	binary.LittleEndian.PutUint32(buf[16:], uint32(2*len(subU)))
	binary.LittleEndian.PutUint32(buf[20:], uint32(2*len(printU)-2))

	copy(buf[24:], unsafeBytes(subU))
	copy(buf[24+2*len(subU):], unsafeBytes(printU))
	return buf
}

func unsafeBytes(u []uint16) []byte {
	if len(u) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), 2*len(u))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Cap reports junction+copy support (see platform.go contract).
func cap() Capability {
	return Capability{JunctionDirs: true, CopyFiles: true}
}
