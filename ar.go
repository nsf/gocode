/*
 * Copyright (c) 2012 The Go Authors. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	arHeader   = "!<arch>\n"
	timeFormat = "Jan _2 15:04 2006"
	// In entryHeader the first entry, the name, is always printed as 16 bytes right-padded.
	entryLen = 16 + 12 + 6 + 6 + 8 + 10 + 1 + 1
)

type Archive os.File

func (ar *Archive) Read(p []byte) (n int, err error) {
	fd := os.File(*ar)
	return fd.Read(p)
}

// Check the header is correct, and move the fd right after.
func (ar *Archive) checkHeader() bool {
	buf := make([]byte, len(arHeader))
	_, err := io.ReadFull(ar, buf)
	return err == nil && string(buf) == arHeader
}

type Entry struct {
	Name    string
	Mtime   int64
	Uid     int
	Gid     int
	Mode    os.FileMode
	Size    int64
	Content string
}

func (e *Entry) String() string {
	return fmt.Sprintf("%s %6d/%-6d %12d %s %s",
		(e.Mode & 0777).String(),
		e.Uid,
		e.Gid,
		e.Size,
		time.Unix(e.Mtime, 0).Format(timeFormat),
		e.Name)
}

func OpenArchive(name string) (*Archive, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	archive := (*Archive)(fd)
	if !archive.checkHeader() {
		return nil, fmt.Errorf("Invalid header found. Not a valid archive?")
	}

	return archive, nil
}

// readEntryContent from Archive (at current location). Set Archive location to next entry.
func (ar *Archive) readEntryContent(size int64) (string, error) {
	buf := make([]byte, size)
	n, err := io.ReadFull(ar, buf)
	if err != nil {
		return "", err
	}
	if n != int(size) {
		return "", err
	}
	if size&1 == 1 {
		fd := os.File(*ar)
		_, err := fd.Seek(1, 1 /*io.SeekCurrent*/)
		if err != nil {
			return "", err
		}
	}

	return string(buf), nil
}

// ReadNextEntry from the archive. Archives points to next location after this.
func (ar *Archive) ReadNextEntry() (*Entry, error) {
	buf := make([]byte, entryLen)
	_, err := io.ReadFull(ar, buf)
	if err == io.EOF {
		// No entries left.
		return nil, nil
	}
	if err != nil || buf[entryLen-2] != '`' || buf[entryLen-1] != '\n' {
		return nil, err
	}
	entry := new(Entry)
	entry.Name = strings.TrimRight(string(buf[:16]), " ")
	if len(entry.Name) == 0 {
		return nil, fmt.Errorf("Entry name is empty")
	}
	buf = buf[16:]
	str := string(buf)
	get := func(width, base, bitsize int) int64 {
		v, err := strconv.ParseInt(strings.TrimRight(str[:width], " "), base, bitsize)
		if err != nil {
			return -1
		}
		str = str[width:]
		return v
	}
	// %-16s%-12d%-6d%-6d%-8o%-10d`
	entry.Mtime = get(12, 10, 64)
	entry.Uid = int(get(6, 10, 32))
	entry.Gid = int(get(6, 10, 32))
	entry.Mode = os.FileMode(get(8, 8, 32))
	entry.Size = get(10, 10, 64)
	entry.Content, err = ar.readEntryContent(entry.Size)
	if err != nil {
		return nil, err
	}
	return entry, nil
}
