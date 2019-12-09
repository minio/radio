// This file is part of Radio
// Copyright (c) 2019 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package streamdup

import (
	"errors"
	"io"
	"sync"

	humanize "github.com/dustin/go-humanize"
	"github.com/minio/minio/pkg/sync/errgroup"
	"github.com/ncw/directio"
)

const readBlockSize = 4 * humanize.MiByte

var streamPool = sync.Pool{
	New: func() interface{} {
		b := directio.AlignedBlock(readBlockSize)
		return &b
	},
}

type multiWriter struct {
	writers []*io.PipeWriter
}

func (t *multiWriter) CloseWithError(err error) error {
	for _, closer := range t.writers {
		closer.CloseWithError(err)
	}
	return err
}

func (t *multiWriter) Close() error {
	for _, closer := range t.writers {
		closer.Close()
	}
	return nil
}
func (t *multiWriter) Write(p []byte) (int, error) {
	g := errgroup.WithNErrs(len(t.writers))
	for index := range t.writers {
		index := index
		g.Go(func() error {
			n, err := t.writers[index].Write(p)
			if err != nil {
				return err
			}
			if n != len(p) {
				return io.ErrShortWrite
			}
			return nil
		}, index)
	}
	for _, err := range g.Wait() {
		if err != nil {
			t.CloseWithError(err)
			return 0, err
		}
	}
	return len(p), nil
}

// New initializes and returns a list of readers
// reach of these readers have a duplicated stream
// of the input reader 'r', as specified by dupN.
func New(r io.Reader, dupN int) ([]io.Reader, error) {
	if dupN < 0 {
		return nil, errors.New("invalid argument")
	}
	if dupN == 0 {
		return []io.Reader{r}, nil
	}
	readers := make([]io.Reader, dupN)
	writers := make([]*io.PipeWriter, dupN)
	for i := range readers {
		readers[i], writers[i] = io.Pipe()
	}
	w := &multiWriter{writers: writers}
	bufp := streamPool.Get().(*[]byte)
	go func() {
		defer streamPool.Put(bufp)
		_, err := io.CopyBuffer(w, r, *bufp)
		w.CloseWithError(err)
	}()
	return readers, nil
}
