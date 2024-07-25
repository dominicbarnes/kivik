// Licensed under the Apache License, Version 2.0 (the "License"); you may not
// use this file except in compliance with the License. You may obtain a copy of
// the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations under
// the License.

//go:build go1.23

package kivik

import (
	"errors"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/go-kivik/kivik/v4/driver"
	"github.com/go-kivik/kivik/v4/int/mock"
)

func TestAttachmentsIterator(t *testing.T) {
	t.Parallel()

	want := []string{"a", "b", "c"}
	var idx int
	atts := &AttachmentsIterator{
		atti: &mock.Attachments{
			NextFunc: func(att *driver.Attachment) error {
				if idx >= len(want) {
					return io.EOF
				}
				att.Filename = want[idx]
				idx++
				return nil
			},
		},
	}

	ids := []string{}
	for att, err := range atts.Iterator() {
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		ids = append(ids, att.Filename)
	}
	if diff := cmp.Diff(want, ids); diff != "" {
		t.Errorf("Unexpected attachments: %s", diff)
	}
}

func TestAttachmentsIteratorError(t *testing.T) {
	t.Parallel()

	atts := &AttachmentsIterator{
		atti: &mock.Attachments{
			NextFunc: func(*driver.Attachment) error {
				return errors.New("failure")
			},
		},
	}

	for _, err := range atts.Iterator() {
		if err == nil {
			t.Fatal("Expected error")
		}
		return
	}
	t.Fatal("Expected an error during iteration")
}

func TestAttachmentsIteratorBreak(t *testing.T) {
	t.Parallel()

	var closed bool
	atts := &AttachmentsIterator{
		atti: &mock.Attachments{
			NextFunc: func(*driver.Attachment) error {
				return nil
			},
		},
		onClose: func() {
			closed = true
		},
	}

	for _, err := range atts.Iterator() {
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		break
	}
	if !closed {
		t.Errorf("Expected iterator to be closed")
	}
}
