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

//go:build !js

package sqlite

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gitlab.com/flimzy/testy"

	"github.com/go-kivik/kivik/v4"
	"github.com/go-kivik/kivik/v4/driver"
	"github.com/go-kivik/kivik/v4/int/mock"
)

func TestDBDeleteAttachment(t *testing.T) {
	t.Parallel()
	type test struct {
		db              *testDB
		docID           string
		filename        string
		options         driver.Options
		wantRev         string
		wantRevs        []leaf
		wantStatus      int
		wantErr         string
		wantAttachments []attachmentRow
	}

	tests := testy.NewTable()
	tests.Add("doc not found", test{
		docID:      "foo",
		filename:   "foo.txt",
		options:    kivik.Rev("1-9bb58f26192e4ba00f01e2e7b136bbd8"),
		wantErr:    "document not found",
		wantStatus: http.StatusNotFound,
	})
	tests.Add("doc exists, but no rev provided", func(t *testing.T) interface{} {
		db := newDB(t)
		_ = db.tPut("foo", map[string]string{"foo": "bar"})

		return test{
			db:         db,
			docID:      "foo",
			filename:   "foo.txt",
			wantErr:    "document update conflict",
			wantStatus: http.StatusConflict,
		}
	})
	tests.Add("doc exists, but wrong rev provided", func(t *testing.T) interface{} {
		db := newDB(t)
		_ = db.tPut("foo", map[string]string{"foo": "bar"})

		return test{
			db:         db,
			docID:      "foo",
			filename:   "foo.txt",
			options:    kivik.Rev("1-wrong"),
			wantErr:    "document update conflict",
			wantStatus: http.StatusConflict,
		}
	})
	tests.Add("success", func(t *testing.T) interface{} {
		d := newDB(t)
		rev := d.tPut("foo", map[string]interface{}{
			"cat":          "meow",
			"_attachments": newAttachments().add("foo.txt", "This is a base64 encoding"),
		})

		return test{
			db:       d,
			docID:    "foo",
			filename: "foo.txt",
			options:  kivik.Rev(rev),
			wantRev:  "2-.*",
			wantRevs: []leaf{
				{
					ID:  "foo",
					Rev: 1,
				},
				{
					ID:        "foo",
					Rev:       2,
					ParentRev: &[]int{1}[0],
				},
			},
			wantAttachments: []attachmentRow{
				{
					DocID:    "foo",
					RevPos:   1,
					Rev:      1,
					Filename: "foo.txt",
					Digest:   "md5-TmfHxaRgUrE9l3tkAn4s0Q==",
				},
			},
		}
	})
	tests.Add("when attempting to delete an attachment that does not exist, a 404 is returned", func(t *testing.T) interface{} {
		d := newDB(t)
		rev := d.tPut("foo", map[string]interface{}{
			"cat": "meow",
		})

		return test{
			db:         d,
			docID:      "foo",
			filename:   "foo.txt",
			options:    kivik.Rev(rev),
			wantErr:    "attachment not found",
			wantStatus: http.StatusNotFound,
			wantRevs:   []leaf{}, // skip checking leaves
		}
	})
	tests.Add("when attempting to delete an attachment, unspecified attachments are unaltered", func(t *testing.T) interface{} {
		d := newDB(t)
		rev := d.tPut("foo", map[string]interface{}{
			"cat": "meow",
			"_attachments": newAttachments().
				add("foo.txt", "This is a base64 encoding").
				add("bar.txt", "This is a base64 encoding"),
		})

		return test{
			db:       d,
			docID:    "foo",
			filename: "foo.txt",
			options:  kivik.Rev(rev),
			wantRevs: []leaf{
				{
					ID:  "foo",
					Rev: 1,
				},
				{
					ID:        "foo",
					Rev:       2,
					ParentRev: &[]int{1}[0],
				},
			},
			wantAttachments: []attachmentRow{
				{
					DocID:    "foo",
					RevPos:   1,
					Rev:      1,
					Filename: "bar.txt",
					Digest:   "md5-TmfHxaRgUrE9l3tkAn4s0Q==",
				},
				{
					DocID:    "foo",
					RevPos:   1,
					Rev:      1,
					Filename: "foo.txt",
					Digest:   "md5-TmfHxaRgUrE9l3tkAn4s0Q==",
				},
				{
					DocID:    "foo",
					RevPos:   1,
					Rev:      2,
					Filename: "bar.txt",
					Digest:   "md5-TmfHxaRgUrE9l3tkAn4s0Q==",
				},
			},
		}
	})
	tests.Add("deleting attachments on non-winning leaf only alters that revision branch", func(t *testing.T) interface{} {
		d := newDB(t)
		rev1 := d.tPut("foo", map[string]interface{}{
			"cat": "meow",
		})
		rev2 := d.tPut("foo", map[string]interface{}{
			"dog":          "woof",
			"_attachments": newAttachments().add("foo.txt", "This is a base64 encoding"),
		}, kivik.Rev(rev1))

		r1, _ := parseRev(rev1)
		r2, _ := parseRev(rev2)

		// Create a conflict
		_ = d.tPut("foo", map[string]interface{}{
			"pig": "oink",
			"_revisions": map[string]interface{}{
				"start": 3,
				"ids":   []string{"abc", "def", r1.id},
			},
		}, kivik.Params(map[string]interface{}{"new_edits": false}))

		return test{
			db:       d,
			docID:    "foo",
			filename: "foo.txt",
			options:  kivik.Rev(rev2),
			wantRev:  "3-.*",
			wantRevs: []leaf{
				{
					ID:    "foo",
					Rev:   1,
					RevID: r1.id,
				},
				{
					ID:          "foo",
					Rev:         2,
					RevID:       "def",
					ParentRev:   &[]int{1}[0],
					ParentRevID: &r1.id,
				},
				{
					ID:          "foo",
					Rev:         2,
					RevID:       r2.id,
					ParentRev:   &[]int{1}[0],
					ParentRevID: &r1.id,
				},
				{
					ID:          "foo",
					Rev:         3,
					ParentRev:   &[]int{2}[0],
					ParentRevID: &r2.id,
				},
				{
					ID:          "foo",
					Rev:         3,
					RevID:       "abc",
					ParentRev:   &[]int{2}[0],
					ParentRevID: &[]string{"def"}[0],
				},
			},
			wantAttachments: []attachmentRow{
				{
					DocID:    "foo",
					RevPos:   2,
					Rev:      2,
					Filename: "foo.txt",
					Digest:   "md5-TmfHxaRgUrE9l3tkAn4s0Q==",
				},
			},
		}
	})

	/*
		TODO:
		- db missing => db not found
	*/

	tests.Run(t, func(t *testing.T, tt test) {
		t.Parallel()
		dbc := tt.db
		if dbc == nil {
			dbc = newDB(t)
		}
		opts := tt.options
		if opts == nil {
			opts = mock.NilOption
		}
		rev, err := dbc.DeleteAttachment(context.Background(), tt.docID, tt.filename, opts)
		if !testy.ErrorMatches(tt.wantErr, err) {
			t.Errorf("Unexpected error: %s", err)
		}
		if status := kivik.HTTPStatus(err); status != tt.wantStatus {
			t.Errorf("Unexpected status: %d", status)
		}
		if err != nil {
			return
		}
		if !regexp.MustCompile(tt.wantRev).MatchString(rev) {
			t.Errorf("Unexpected rev: %s, want %s", rev, tt.wantRev)
		}
		switch {
		case tt.wantRevs == nil:
			t.Errorf("No leaves to check")
		case len(tt.wantRevs) == 0:
			// Do nothing
		default:
			leaves := readRevisions(t, dbc.underlying())
			for i, r := range tt.wantRevs {
				// allow tests to omit RevID
				if r.RevID == "" {
					leaves[i].RevID = ""
				}
				if r.ParentRevID == nil {
					leaves[i].ParentRevID = nil
				}
			}
			if d := cmp.Diff(tt.wantRevs, leaves); d != "" {
				t.Errorf("Unexpected leaves: %s", d)
			}
		}
		checkAttachments(t, dbc.underlying(), tt.wantAttachments)
	})
}
