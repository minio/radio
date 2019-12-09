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
	"bytes"
	"io"
	"io/ioutil"
	"sync"
	"testing"

	humanize "github.com/dustin/go-humanize"
)

func TestNew(t *testing.T) {
	r := bytes.NewReader([]byte("Hello, World!"))
	larger := bytes.NewReader(bytes.Repeat([]byte("10101010"), 4*humanize.MiByte))

	testCases := []struct {
		r               *bytes.Reader
		dupN            int
		expectedSuccess bool
	}{
		{r, -1, false},
		{r, 0, true},
		{r, 3, true},
		{r, 27, true},
		{larger, 3, true},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run("", func(t *testing.T) {
			testCase.r.Seek(0, io.SeekStart)
			readers, err := New(testCase.r, testCase.dupN)
			if err != nil && testCase.expectedSuccess {
				t.Errorf("Expected success but found failure %s", err)
			}
			if err == nil && !testCase.expectedSuccess {
				t.Errorf("Expected failure but found success")
			}

			var wg sync.WaitGroup
			for _, rd := range readers {
				wg.Add(1)
				go func(rd io.Reader) {
					defer wg.Done()
					n, err := io.Copy(ioutil.Discard, rd)
					if err != nil {
						t.Errorf("Expected success on read, but for failure %s", err)
					}
					if testCase.r.Size() != n {
						t.Errorf("Expected data %d, got %d", testCase.r.Len(), n)
					}
				}(rd)
			}
			wg.Wait()
		})
	}
}
