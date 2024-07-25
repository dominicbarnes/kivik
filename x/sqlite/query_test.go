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
	"testing"
	"time"

	"gitlab.com/flimzy/testy"

	"github.com/go-kivik/kivik/v4"
	"github.com/go-kivik/kivik/v4/driver"
	"github.com/go-kivik/kivik/v4/int/mock"
)

func TestDBQuery(t *testing.T) {
	t.Parallel()
	type test struct {
		db            *testDB
		ddoc, view    string
		options       driver.Options
		want          []rowResult
		wantUnordered bool
		wantStatus    int
		wantErr       string
		wantLogs      []string
	}
	tests := testy.NewTable()
	tests.Add("ddoc does not exist", test{
		ddoc:       "_design/foo",
		wantErr:    "missing",
		wantStatus: http.StatusNotFound,
	})
	tests.Add("ddoc does exist but view does not", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]string{"cat": "meow"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    "missing named view",
			wantStatus: http.StatusNotFound,
		}
	})
	tests.Add("ddoc does exist but only non-view functions exist", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{"updates": map[string]string{"update1": "function() {}"}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    "missing named view",
			wantStatus: http.StatusNotFound,
		}
	})
	tests.Add("simple view with a single document", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		_ = d.tPut("foo", map[string]string{"_id": "foo"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "foo", Key: `"foo"`, Value: "null"},
			},
		}
	})
	tests.Add("invalid update value", test{
		options:    kivik.Param("update", "foo"),
		wantErr:    "invalid value for 'update'",
		wantStatus: http.StatusBadRequest,
	})
	tests.Add("with update=false", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		_ = d.tPut("foo", map[string]string{"_id": "foo"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("update", false),
			want:    nil,
		}
	})
	tests.Add("explicit update=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		_ = d.tPut("foo", map[string]string{"_id": "foo"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("update", true),
			want: []rowResult{
				{ID: "foo", Key: `"foo"`, Value: "null"},
			},
		}
	})
	tests.Add("Updating ddoc renders index obsolete", func(t *testing.T) interface{} {
		d := newDB(t)
		rev := d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		_ = d.tPut("foo", map[string]string{"_id": "foo"})
		// Ensure the index is built
		rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", mock.NilOption)
		if err != nil {
			t.Fatalf("Failed to query view: %s", err)
		}
		_ = rows.Close()

		// Update the ddoc
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(x) { emit(x._id, null); }`,
				},
			},
		}, kivik.Rev(rev))

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("update", false),
			want:    nil,
		}
	})
	tests.Add("incremental update", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		_ = d.tPut("foo", map[string]string{"_id": "foo"})
		// Ensure the index is built
		rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", mock.NilOption)
		if err != nil {
			t.Fatalf("Failed to query view: %s", err)
		}
		_ = rows.Close()

		// Add a new doc to trigger an incremental update
		_ = d.tPut("bar", map[string]interface{}{"_id": "bar"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "bar", Key: `"bar"`, Value: "null"},
				{ID: "foo", Key: `"foo"`, Value: "null"},
			},
		}
	})
	tests.Add("ddoc does not exist with update=false", test{
		ddoc:       "_design/foo",
		options:    kivik.Param("update", false),
		wantErr:    "missing",
		wantStatus: http.StatusNotFound,
	})
	tests.Add("ddoc does exist but view does not with update=false", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]string{"cat": "meow"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			options:    kivik.Param("update", false),
			wantErr:    "missing named view",
			wantStatus: http.StatusNotFound,
		}
	})
	tests.Add("deleting doc deindexes it", func(t *testing.T) interface{} {
		d := newDB(t)
		rev := d.tPut("foo", map[string]string{"cat": "meow"})

		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		// Ensure the index is built
		rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", mock.NilOption)
		if err != nil {
			t.Fatalf("Failed to query view: %s", err)
		}
		_ = rows.Close()

		// Add a new doc to trigger an incremental update
		_ = d.tDelete("foo", kivik.Rev(rev))

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: nil,
		}
	})
	tests.Add("doc deleted before index creation", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) { emit(doc._id, null); }`,
				},
			},
		})
		rev := d.tPut("foo", map[string]string{"_id": "foo"})
		_ = d.tDelete("foo", kivik.Rev(rev))

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: nil,
		}
	})
	tests.Add("map function throws exception", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("foo", map[string]string{"cat": "meow"})
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
						emit(doc._id, null);
						throw new Error("broken");
					}`,
				},
			},
		})
		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: nil,
			wantLogs: []string{
				"^map function threw exception for foo: Error: broken$",
				"^\tat map ",
			},
		}
	})
	tests.Add("emit function throws exception", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("foo", map[string]string{"cat": "meow"})
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
						emit(doc._id, function() {});
					}`,
				},
			},
		})
		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: nil,
			wantLogs: []string{
				`^map function emitted invalid value '0x[\da-f]+': json: unsupported type: func\(goja\.FunctionCall\) goja\.Value$`,
			},
		}
	})
	tests.Add("map that references attachments", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
						if (doc._attachments) { // Check if there are attachments
							for (var filename in doc._attachments) { // Loop over all attachments
								emit(filename); // Emit the attachment filename
							}
						}
					}`,
				},
			},
		})
		_ = d.tPut("no_attachments", map[string]string{"foo": "bar"})
		_ = d.tPut("with_attachments", map[string]interface{}{
			"_attachments": newAttachments().
				add("foo.txt", "Hello, World!").
				add("bar.txt", "Goodbye, World!"),
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "with_attachments", Key: `"bar.txt"`, Value: "null"},
				{ID: "with_attachments", Key: `"foo.txt"`, Value: "null"},
			},
		}
	})
	tests.Add("default CouchDB collation", func(t *testing.T) interface{} {
		// See https://docs.couchdb.org/en/stable/ddocs/views/collation.html
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key !== undefined) {
								emit(doc.key, null);
							}
						}`,
				},
			},
		})
		_ = d.tPut("null", map[string]interface{}{"key": nil})
		_ = d.tPut("bool: false", map[string]interface{}{"key": false})
		_ = d.tPut("bool: true", map[string]interface{}{"key": true})
		_ = d.tPut("numbers: 1", map[string]interface{}{"key": int(1)})
		_ = d.tPut("numbers: 2", map[string]interface{}{"key": int(2)})
		_ = d.tPut("numbers: 3.0", map[string]interface{}{"key": float64(3)})
		_ = d.tPut("numbers: 4", map[string]interface{}{"key": int(4)})
		_ = d.tPut("text: a", map[string]interface{}{"key": "a"})
		_ = d.tPut("text: A", map[string]interface{}{"key": "A"})
		_ = d.tPut("text: aa", map[string]interface{}{"key": "aa"})
		_ = d.tPut("text: b", map[string]interface{}{"key": "b"})
		_ = d.tPut("text: B", map[string]interface{}{"key": "B"})
		_ = d.tPut("text: ba", map[string]interface{}{"key": "ba"})
		_ = d.tPut("text: bb", map[string]interface{}{"key": "bb"})
		_ = d.tPut("array: [a]", map[string]interface{}{"key": []string{"a"}})
		_ = d.tPut("array: [b]", map[string]interface{}{"key": []string{"b"}})
		_ = d.tPut("array: [b, c]", map[string]interface{}{"key": []string{"b", "c"}})
		_ = d.tPut("array: [b, c, a]", map[string]interface{}{"key": []string{"b", "c", "a"}})
		_ = d.tPut("array: [b, d]", map[string]interface{}{"key": []string{"b", "d"}})
		_ = d.tPut("array: [b, d, e]", map[string]interface{}{"key": []string{"b", "d", "e"}})
		_ = d.tPut("object: {a:1}", map[string]interface{}{"key": map[string]interface{}{"a": 1}})
		_ = d.tPut("object: {a:2}", map[string]interface{}{"key": map[string]interface{}{"a": 2}})
		_ = d.tPut("object: {b:1}", map[string]interface{}{"key": map[string]interface{}{"b": 1}})
		// _ = d.tPut("object: {b:2, a:1}", map[string]interface{}{"key": map[string]interface{}{"b": 2, "a": 1}})
		_ = d.tPut("object: {b:2, c:2}", map[string]interface{}{"key": map[string]interface{}{"b": 2, "c": 2}})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("update", true),
			want: []rowResult{
				{ID: "null", Key: `null`, Value: "null"},
				{ID: "bool: false", Key: `false`, Value: "null"},
				{ID: "bool: true", Key: `true`, Value: "null"},
				{ID: "numbers: 1", Key: `1`, Value: "null"},
				{ID: "numbers: 2", Key: `2`, Value: "null"},
				{ID: "numbers: 3.0", Key: `3`, Value: "null"},
				{ID: "numbers: 4", Key: `4`, Value: "null"},
				{ID: "text: a", Key: `"a"`, Value: "null"},
				{ID: "text: A", Key: `"A"`, Value: "null"},
				{ID: "text: aa", Key: `"aa"`, Value: "null"},
				{ID: "text: b", Key: `"b"`, Value: "null"},
				{ID: "text: B", Key: `"B"`, Value: "null"},
				{ID: "text: ba", Key: `"ba"`, Value: "null"},
				{ID: "text: bb", Key: `"bb"`, Value: "null"},
				{ID: "array: [a]", Key: `["a"]`, Value: "null"},
				{ID: "array: [b]", Key: `["b"]`, Value: "null"},
				{ID: "array: [b, c]", Key: `["b","c"]`, Value: "null"},
				{ID: "array: [b, c, a]", Key: `["b","c","a"]`, Value: "null"},
				{ID: "array: [b, d]", Key: `["b","d"]`, Value: "null"},
				{ID: "array: [b, d, e]", Key: `["b","d","e"]`, Value: "null"},
				{ID: "object: {a:1}", Key: `{"a":1}`, Value: "null"},
				{ID: "object: {a:2}", Key: `{"a":2}`, Value: "null"},
				{ID: "object: {b:1}", Key: `{"b":1}`, Value: "null"},
				// {ID: "object: {b:2, a:1}", Key: `{"a":1,"b":2}`, Value: "null"}, // Object order  is not honored by goja. May be possible to fix with great effort.
				{ID: "object: {b:2, c:2}", Key: `{"b":2,"c":2}`, Value: "null"},
			},
		}
	})
	tests.Add("ASCII collation", func(t *testing.T) interface{} {
		// See https://docs.couchdb.org/en/stable/ddocs/views/collation.html
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key !== undefined) {
								emit(doc.key, null);
							}
						}`,
				},
			},
			"options": map[string]interface{}{
				"collation": "ascii",
			},
		})
		_ = d.tPut("null", map[string]interface{}{"key": nil})
		_ = d.tPut("bool: false", map[string]interface{}{"key": false})
		_ = d.tPut("bool: true", map[string]interface{}{"key": true})
		_ = d.tPut("numbers: 1", map[string]interface{}{"key": int(1)})
		_ = d.tPut("numbers: 2", map[string]interface{}{"key": int(2)})
		_ = d.tPut("numbers: 3.0", map[string]interface{}{"key": float64(3)})
		_ = d.tPut("numbers: 4", map[string]interface{}{"key": int(4)})
		_ = d.tPut("text: a", map[string]interface{}{"key": "a"})
		_ = d.tPut("text: A", map[string]interface{}{"key": "A"})
		_ = d.tPut("text: aa", map[string]interface{}{"key": "aa"})
		_ = d.tPut("text: b", map[string]interface{}{"key": "b"})
		_ = d.tPut("text: B", map[string]interface{}{"key": "B"})
		_ = d.tPut("text: ba", map[string]interface{}{"key": "ba"})
		_ = d.tPut("text: bb", map[string]interface{}{"key": "bb"})
		_ = d.tPut("array: [a]", map[string]interface{}{"key": []string{"a"}})
		_ = d.tPut("array: [b]", map[string]interface{}{"key": []string{"b"}})
		_ = d.tPut("array: [b, c]", map[string]interface{}{"key": []string{"b", "c"}})
		_ = d.tPut("array: [b, c, a]", map[string]interface{}{"key": []string{"b", "c", "a"}})
		_ = d.tPut("array: [b, d]", map[string]interface{}{"key": []string{"b", "d"}})
		_ = d.tPut("array: [b, d, e]", map[string]interface{}{"key": []string{"b", "d", "e"}})
		_ = d.tPut("object: {a:1}", map[string]interface{}{"key": map[string]interface{}{"a": 1}})
		_ = d.tPut("object: {a:2}", map[string]interface{}{"key": map[string]interface{}{"a": 2}})
		_ = d.tPut("object: {b:1}", map[string]interface{}{"key": map[string]interface{}{"b": 1}})
		// _ = d.tPut("object: {b:2, a:1}", map[string]interface{}{"key": map[string]interface{}{"b": 2, "a": 1}})
		_ = d.tPut("object: {b:2, c:2}", map[string]interface{}{"key": map[string]interface{}{"b": 2, "c": 2}})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("update", true),
			want: []rowResult{
				{ID: "null", Key: `null`, Value: "null"},
				{ID: "text: A", Key: `"A"`, Value: "null"},
				{ID: "text: B", Key: `"B"`, Value: "null"},
				{ID: "text: a", Key: `"a"`, Value: "null"},
				{ID: "text: aa", Key: `"aa"`, Value: "null"},
				{ID: "text: b", Key: `"b"`, Value: "null"},
				{ID: "text: ba", Key: `"ba"`, Value: "null"},
				{ID: "text: bb", Key: `"bb"`, Value: "null"},
				{ID: "numbers: 1", Key: `1`, Value: "null"},
				{ID: "numbers: 2", Key: `2`, Value: "null"},
				{ID: "numbers: 3.0", Key: `3`, Value: "null"},
				{ID: "numbers: 4", Key: `4`, Value: "null"},
				{ID: "array: [a]", Key: `["a"]`, Value: "null"},
				{ID: "array: [b, c, a]", Key: `["b","c","a"]`, Value: "null"},
				{ID: "array: [b, c]", Key: `["b","c"]`, Value: "null"},
				{ID: "array: [b, d, e]", Key: `["b","d","e"]`, Value: "null"},
				{ID: "array: [b, d]", Key: `["b","d"]`, Value: "null"},
				{ID: "array: [b]", Key: `["b"]`, Value: "null"},
				{ID: "bool: false", Key: `false`, Value: "null"},
				{ID: "bool: true", Key: `true`, Value: "null"},
				{ID: "object: {a:1}", Key: `{"a":1}`, Value: "null"},
				{ID: "object: {a:2}", Key: `{"a":2}`, Value: "null"},
				{ID: "object: {b:1}", Key: `{"b":1}`, Value: "null"},
				// {ID: "object: {b:2, a:1}", Key: `{"a":1,"b":2}`, Value: "null"}, // Object order  is not honored by goja. May be possible to fix with great effort.
				{ID: "object: {b:2, c:2}", Key: `{"b":2,"c":2}`, Value: "null"},
			},
		}
	})
	tests.Add("simple reduce function", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					// Manual implementation of _count for testing purposes.
					"reduce": `function(sum, values, rereduce) {
							if (rereduce) {
								let sum=0;
								for (let i=0; i < values.length; i++) {
									sum += values[i];
								}
								return sum;
							}
							return values.length;
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{
					Key:   "null",
					Value: "2",
				},
			},
		}
	})
	tests.Add("reduce=true for map-only view returns 400", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			options:    kivik.Param("reduce", true),
			wantErr:    "reduce is invalid for map-only views",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("simple reduce function with reduce=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					// Manual implementation of _count for testing purposes.
					"reduce": `function(sum, values, rereduce) {
							if (rereduce) {
								let sum=0;
								for (let i=0; i < values.length; i++) {
									sum += values[i];
								}
								return sum;
							}
							return values.length;
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("reduce", true),
			want: []rowResult{
				{
					Key:   "null",
					Value: "2",
				},
			},
		}
	})
	tests.Add("simple reduce function with reduce=false", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					// Manual implementation of _count for testing purposes.
					"reduce": `function(sum, values, rereduce) {
							if (rereduce) {
								let sum=0;
								for (let i=0; i < values.length; i++) {
									sum += values[i];
								}
								return sum;
							}
							return values.length;
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("reduce", false),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: `[1]`},
				{ID: "b", Key: `"b"`, Value: `[1]`},
			},
		}
	})
	tests.Add("reduce function throws an exception", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `function(sum, values, rereduce) {
							throw new Error("broken");
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{
					Key:   "null",
					Value: "null",
				},
			},
			wantLogs: []string{
				`^reduce function threw exception: Error: broken`,
				`at reduce`,
			},
		}
	})
	tests.Add("built-in _count reduce function", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{
					Key:   "null",
					Value: "2",
				},
			},
		}
	})
	tests.Add("built-in _sum reduce function", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, 3);
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{
					Key:   "null",
					Value: "6",
				},
			},
		}
	})
	tests.Add("malformed reduce param", test{
		options:    kivik.Param("reduce", "foo"),
		wantErr:    "invalid value for 'reduce'",
		wantStatus: http.StatusBadRequest,
	})
	tests.Add("malformed group param", test{
		options:    kivik.Param("group", "foo"),
		wantErr:    "invalid value for 'group'",
		wantStatus: http.StatusBadRequest,
	})
	tests.Add("malformed group_level param", test{
		options:    kivik.Param("group_level", "foo"),
		wantErr:    "invalid value for 'group_level'",
		wantStatus: http.StatusBadRequest,
	})
	tests.Add("group=true for map-only view returns 400", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			options:    kivik.Param("group", true),
			wantErr:    "group is invalid for map-only views",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("simple group=true case", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, 1);
							}
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("A", map[string]string{"key": "a"})
		_ = d.tPut("b", map[string]string{"key": "b"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("group", "true"),
			want: []rowResult{
				{Key: `"a"`, Value: "2"},
				{Key: `"b"`, Value: "1"},
			},
		}
	})
	tests.Add("group=true with null key", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(null, 1);
							}
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("A", map[string]string{"key": "a"})
		_ = d.tPut("b", map[string]string{"key": "b"})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("group", "true"),
			want: []rowResult{
				{Key: `null`, Value: "3"},
			},
		}
	})
	tests.Add("group_level=2 for map-only view returns 400", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"a": "a"})
		_ = d.tPut("b", map[string]string{"b": "b"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			options:    kivik.Param("group_level", 2),
			wantErr:    "group_level is invalid for map-only views",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("group_level above maximum", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, 1);
							}
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("A", map[string]string{"key": "a"})
		_ = d.tPut("ab", map[string]interface{}{"key": []string{"a", "b"}})
		_ = d.tPut("aa", map[string]interface{}{"key": []string{"a", "a"}})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("group_level", 100),
			want: []rowResult{
				{Key: `"a"`, Value: "2"},
				{Key: `["a","a"]`, Value: "1"},
				{Key: `["a","b"]`, Value: "1"},
			},
		}
	})
	tests.Add("group_level=1", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, 1);
							}
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("A", map[string]string{"key": "a"})
		_ = d.tPut("ab", map[string]interface{}{"key": []string{"a", "b"}})
		_ = d.tPut("aa", map[string]interface{}{"key": []string{"a", "a"}})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("group_level", 1),
			want: []rowResult{
				{Key: `"a"`, Value: "2"},
				{Key: `["a"]`, Value: "2"},
			},
		}
	})
	tests.Add("group_level=0 works same as no grouping", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, 1);
							}
						}`,
					"reduce": `_sum`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("A", map[string]string{"key": "a"})
		_ = d.tPut("ab", map[string]interface{}{"key": []string{"a", "b"}})
		_ = d.tPut("aa", map[string]interface{}{"key": []string{"a", "a"}})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("group_level", 0),
			want: []rowResult{
				{Key: `null`, Value: "4"},
			},
		}
	})
	tests.Add("_stats with single numeric value from map function", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, 1);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]string{"key": "a"})
		_ = d.tPut("b", map[string]string{"key": "b"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{Key: `null`, Value: `{"sum":2,"min":1,"max":1,"count":2,"sumsqr":2}`},
			},
		}
	})
	tests.Add("_stats with negative and positive values", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": -1})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{Key: `null`, Value: `{"sum":0,"min":-1,"max":1,"count":2,"sumsqr":2}`},
			},
		}
	})
	tests.Add("_stats with floating point values", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1.23})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": -1.23})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{Key: `null`, Value: `{"sum":0,"min":-1.23,"max":1.23,"count":2,"sumsqr":3.0258}`},
			},
		}
	})
	tests.Add("_stats with non numeric values", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1.23})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": "dog"})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    "the _stats function requires that map values be numbers or arrays of numbers, not '\"dog\"'",
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with null value", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": nil})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    "the _stats function requires that map values be numbers or arrays of numbers, not 'null'",
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with array of strings value", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": []string{"dog", "cat", "cow"}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `the _stats function requires that map values be numbers or arrays of numbers, not '\["dog","cat","cow"\]'`,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with pre-aggregated value", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "b", "value": 100})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{
			"sum":     5,
			"min":     5,
			"max":     5,
			"count":   5,
			"sumsqr":  5,
			"ignored": 5,
		}})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{Key: `null`, Value: `{"sum":105,"min":5,"max":100,"count":6,"sumsqr":10005}`},
			},
		}
	})
	tests.Add("_stats with partial pre-aggregated value, no count", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{
			"sum": 5,
		}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `user _stats input missing required field count \({"sum":5}\)`,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with partial pre-aggregated value, no min", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{
			"sum":   5,
			"max":   5,
			"count": 5,
		}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `^user _stats input missing required field min `,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with partial pre-aggregated value, no max", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{
			"sum":   5,
			"count": 5,
			"min":   5,
		}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `^user _stats input missing required field max `,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with partial pre-aggregated value, no sumsqr", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{
			"sum":   5,
			"count": 5,
			"min":   5,
			"max":   5,
		}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `^user _stats input missing required field sumsqr `,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with empty pre-aggregated value", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": map[string]float64{}})

		return test{
			db:         d,
			ddoc:       "_design/foo",
			view:       "_view/bar",
			wantErr:    `^user _stats input missing required field count `,
			wantStatus: http.StatusInternalServerError,
		}
	})
	tests.Add("_stats with arrays of numbers", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, [1,2,3,4]);
							}
						}`,
					"reduce": `_stats`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": map[string]float64{}})
		_ = d.tPut("b", map[string]interface{}{"key": "a", "value": map[string]float64{}})
		_ = d.tPut("c", map[string]interface{}{"key": "a", "value": map[string]float64{}})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{Key: `null`, Value: `[{"sum":3,"min":1,"max":1,"count":3,"sumsqr":3},{"sum":6,"min":2,"max":2,"count":3,"sumsqr":12},{"sum":9,"min":3,"max":3,"count":3,"sumsqr":27},{"sum":12,"min":4,"max":4,"count":3,"sumsqr":48}]`},
			},
		}
	})
	tests.Add("limit=1", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("limit", 1),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "1"},
			},
		}
	})
	tests.Add("limit=1, skip=1", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("c", map[string]interface{}{"key": "c", "value": 3})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"limit": 1,
				"skip":  1,
			}),
			want: []rowResult{
				{ID: "b", Key: `"b"`, Value: "2"},
			},
		}
	})
	tests.Add("include_docs=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		rev := d.tPut("a", map[string]interface{}{"key": "a", "value": 1})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("include_docs", true),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "1", Doc: `{"_id":"a","_rev":"` + rev + `","key":"a","value":1}`},
			},
		}
	})
	tests.Add("views should only include winning rev in case of conflict", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{
			"key":   "a",
			"value": 1,
			"_rev":  "1-abc",
		}, kivik.Param("new_edits", false))
		_ = d.tPut("a", map[string]interface{}{
			"key":   "a",
			"value": 2,
			"_rev":  "1-xyz",
		}, kivik.Param("new_edits", false))

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "2"},
			},
		}
	})
	tests.Add("conflicts=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{
			"key":   "a",
			"value": 1,
			"_rev":  "1-abc",
		}, kivik.Param("new_edits", false))
		_ = d.tPut("a", map[string]interface{}{
			"key":   "a",
			"value": 2,
			"_rev":  "1-xyz",
		}, kivik.Param("new_edits", false))

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"include_docs": true,
				"conflicts":    true,
			}),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "2", Doc: `{"_id":"a","_rev":"1-xyz","key":"a","value":2,"_conflicts":["1-abc"]}`},
			},
		}
	})
	tests.Add("endkey", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("c", map[string]interface{}{"key": "c", "value": 3})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey": "b",
			}),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "1"},
				{ID: "b", Key: `"b"`, Value: "2"},
			},
		}
	})
	tests.Add("startkey", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("c", map[string]interface{}{"key": "c", "value": 3})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"startkey": "b",
			}),
			want: []rowResult{
				{ID: "b", Key: `"b"`, Value: "2"},
				{ID: "c", Key: `"c"`, Value: "3"},
			},
		}
	})
	tests.Add("return unsorted results", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		// Returned results are implicitly ordered, due to the ordering of
		// the index on the `id` column for joins. So we need to insert
		// a key that sorts differently implicitly (with ASCII ordering) than
		// explicitly (with CouchDB's UCI ordering). Thus the `~` id/key.  In
		// ASCII, ~ comes after the alphabet, in UCI it comes first.  So we
		// expect it to come after, with implicit ordering, or after, with
		// explicit. Comment out the options line below to see the difference.
		_ = d.tPut("~", map[string]interface{}{"key": "~", "value": 3})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("sorted", false),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "1"},
				{ID: "b", Key: `"b"`, Value: "2"},
				{ID: "~", Key: `"~"`, Value: "3"},
			},
		}
	})
	tests.Add("group=true, unordered results", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("~", map[string]interface{}{})
		_ = d.tPut("a", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"sorted": false,
				"group":  true,
			}),
			wantUnordered: true,
			want: []rowResult{
				{Key: `"~"`, Value: "1"},
				{Key: `"a"`, Value: "1"},
			},
		}
	})
	tests.Add("group=true, ascending order", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("~", map[string]interface{}{})
		_ = d.tPut("a", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group": true,
			}),
			want: []rowResult{
				{Key: `"~"`, Value: "1"},
				{Key: `"a"`, Value: "1"},
			},
		}
	})
	tests.Add("group=true, descending order", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("~", map[string]interface{}{})
		_ = d.tPut("a", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group":      true,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"a"`, Value: "1"},
				{Key: `"~"`, Value: "1"},
			},
		}
	})
	tests.Add("support fetching specific key", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("c", map[string]interface{}{"key": "c", "value": 3})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"key": "b",
			}),
			want: []rowResult{
				{ID: "b", Key: `"b"`, Value: "2"},
			},
		}
	})
	tests.Add("support fetching multiple specific keys", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a", "value": 1})
		_ = d.tPut("b", map[string]interface{}{"key": "b", "value": 2})
		_ = d.tPut("c", map[string]interface{}{"key": "c", "value": 3})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"keys": []string{"a", "b"},
			}),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "1"},
				{ID: "b", Key: `"b"`, Value: "2"},
			},
		}
	})
	tests.Add("endkey_docid", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, null);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a"})
		_ = d.tPut("b", map[string]interface{}{"key": "a"})
		_ = d.tPut("c", map[string]interface{}{"key": "a"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey":       "a",
				"endkey_docid": "b",
			}),
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "null"},
				{ID: "b", Key: `"a"`, Value: "null"},
			},
		}
	})
	tests.Add("startkey_docid", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a"})
		_ = d.tPut("b", map[string]interface{}{"key": "a"})
		_ = d.tPut("c", map[string]interface{}{"key": "a"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"startkey":       "a",
				"startkey_docid": "b",
			}),
			want: []rowResult{
				{ID: "b", Key: `"a"`, Value: "null"},
				{ID: "c", Key: `"a"`, Value: "null"},
			},
		}
	})
	tests.Add("include design docs in view output", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"key": "design",
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.value);
							}
						}`,
				},
			},
			"options": map[string]interface{}{
				"include_design": true,
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "null"},
				{ID: "_design/foo", Key: `"design"`, Value: "null"},
			},
		}
	})
	tests.Add("options.local_seq=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"key": "design",
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc._local_seq);
							}
						}`,
				},
			},
			"options": map[string]interface{}{
				"local_seq": true,
			},
		})
		_ = d.tPut("a", map[string]interface{}{"key": "a"})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			want: []rowResult{
				{ID: "a", Key: `"a"`, Value: "2"},
			},
		}
	})
	tests.Add("attachments=false, one attachment", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"key": "design",
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.key);
							}
						}`,
				},
			},
		})
		rev := d.tPut("a", map[string]interface{}{
			"key":          "a",
			"_attachments": newAttachments().add("foo.txt", "foo"),
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"attachments":  false,
				"include_docs": true,
			}),
			want: []rowResult{
				{
					ID:    "a",
					Key:   `"a"`,
					Value: `"a"`,
					Doc:   `{"_id":"a","_rev":"` + rev + `","key":"a","_attachments":{"foo.txt":{"content_type":"text/plain","digest":"md5-rL0Y20zC+Fzt72VPzMSk2A==","length":3,"revpos":1,"stub":true}}}`,
				},
			},
		}
	})
	tests.Add("attachments=true, one attachment", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"key": "design",
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.key);
							}
						}`,
				},
			},
		})
		rev := d.tPut("a", map[string]interface{}{
			"key":          "a",
			"_attachments": newAttachments().add("foo.txt", "foo"),
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"attachments":  true,
				"include_docs": true,
			}),
			want: []rowResult{
				{
					ID:    "a",
					Key:   `"a"`,
					Value: `"a"`,
					Doc:   `{"_id":"a","_rev":"` + rev + `","key":"a","_attachments":{"foo.txt":{"content_type":"text/plain","digest":"md5-rL0Y20zC+Fzt72VPzMSk2A==","length":3,"revpos":1,"data":"Zm9v"}}}`,
				},
			},
		}
	})
	tests.Add("attachments=true, two attachment", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"key": "design",
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							if (doc.key) {
								emit(doc.key, doc.key);
							}
						}`,
				},
			},
		})
		rev := d.tPut("a", map[string]interface{}{
			"key": "a",
			"_attachments": newAttachments().
				add("foo.txt", "foo").
				add("bar.txt", "bar"),
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"attachments":  true,
				"include_docs": true,
			}),
			want: []rowResult{
				{
					ID:    "a",
					Key:   `"a"`,
					Value: `"a"`,
					Doc:   `{"_id":"a","_rev":"` + rev + `","key":"a","_attachments":{"bar.txt":{"content_type":"text/plain","digest":"md5-N7UdGUp1E+RbVvZSTy1R8g==","length":3,"revpos":1,"data":"YmFy"},"foo.txt":{"content_type":"text/plain","digest":"md5-rL0Y20zC+Fzt72VPzMSk2A==","length":3,"revpos":1,"data":"Zm9v"}}}`,
				},
			},
		}
	})
	tests.Add("include_docs is invalid for reduce=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"reduce":       true,
				"include_docs": true,
			}),
			wantErr:    "include_docs is invalid for reduce",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("include_docs is invalid for implicit reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"include_docs": true,
			}),
			wantErr:    "include_docs is invalid for reduce",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("conflicts is invalid for reduce=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"reduce":    true,
				"conflicts": true,
			}),
			wantErr:    "conflicts is invalid for reduce",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("conflicts is invalid for implicit reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"conflicts": true,
			}),
			wantErr:    "conflicts is invalid for reduce",
			wantStatus: http.StatusBadRequest,
		}
	})
	tests.Add("endkey with reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("endkey", "b"),
			want:    []rowResult{{Key: `null`, Value: `2`}},
		}
	})
	tests.Add("startkey with reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("startkey", "b"),
			want:    []rowResult{{Key: `null`, Value: `3`}},
		}
	})
	tests.Add("startkey with reduce and descending", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"startkey":   "b",
				"descending": true,
			}),
			want: []rowResult{{Key: `null`, Value: `2`}},
		}
	})
	tests.Add("key with reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("key", "b"),
			want:    []rowResult{{Key: `null`, Value: `1`}},
		}
	})
	tests.Add("keys with reduce", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:      d,
			ddoc:    "_design/foo",
			view:    "_view/bar",
			options: kivik.Param("keys", []string{"b", "c"}),
			want:    []rowResult{{Key: `null`, Value: `2`}},
		}
	})
	tests.Add("endkey with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey": "b",
				"group":  true,
			}),
			want: []rowResult{
				{Key: `"a"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
			},
		}
	})
	tests.Add("endkey with group=true and descending", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey":     "b",
				"group":      true,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"d"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
			},
		}
	})
	tests.Add("startkey with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"startkey": "b",
				"group":    true,
			}),
			want: []rowResult{
				{Key: `"b"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
				{Key: `"d"`, Value: `1`},
			},
		}
	})
	tests.Add("startkey with group=true and descending", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"startkey":   "b",
				"group":      true,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"b"`, Value: `1`},
				{Key: `"a"`, Value: `1`},
			},
		}
	})
	tests.Add("endkey with reduce=true, descending, and inclusive_end=false", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey":        "b",
				"descending":    true,
				"inclusive_end": false,
			}),
			want: []rowResult{
				{Key: `null`, Value: `2`},
			},
		}
	})
	tests.Add("endkey with reduce=true and inclusive_end=false", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"endkey":        "b",
				"inclusive_end": false,
			}),
			want: []rowResult{
				{Key: `null`, Value: `1`},
			},
		}
	})
	tests.Add("key with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"key":   "b",
				"group": true,
			}),
			want: []rowResult{
				{Key: `"b"`, Value: `1`},
			},
		}
	})
	tests.Add("keys with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"keys":  []string{"b", "c"},
				"group": true,
			}),
			want: []rowResult{
				{Key: `"b"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
			},
		}
	})
	tests.Add("keys with group=true, descending=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"keys":       []string{"b", "c"},
				"group":      true,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"c"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
			},
		}
	})
	tests.Add("limit with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group": true,
				"limit": 3,
			}),
			want: []rowResult{
				{Key: `"a"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
			},
		}
	})
	tests.Add("skip with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group": true,
				"skip":  3,
			}),
			want: []rowResult{
				{Key: `"d"`, Value: `1`},
			},
		}
	})
	tests.Add("limit with group=true and descending", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group":      true,
				"limit":      3,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"d"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
			},
		}
	})
	tests.Add("skip with group=true and descending", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group":      true,
				"skip":       3,
				"descending": true,
			}),
			want: []rowResult{
				{Key: `"a"`, Value: `1`},
			},
		}
	})
	tests.Add("sorted=false with group=true", func(t *testing.T) interface{} {
		d := newDB(t)
		_ = d.tPut("_design/foo", map[string]interface{}{
			"views": map[string]interface{}{
				"bar": map[string]string{
					"map": `function(doc) {
							emit(doc._id, [1]);
						}`,
					"reduce": `_count`,
				},
			},
		})
		_ = d.tPut("c", map[string]interface{}{})
		_ = d.tPut("a", map[string]interface{}{})
		_ = d.tPut("d", map[string]interface{}{})
		_ = d.tPut("b", map[string]interface{}{})

		return test{
			db:   d,
			ddoc: "_design/foo",
			view: "_view/bar",
			options: kivik.Params(map[string]interface{}{
				"group":  true,
				"sorted": false,
			}),
			// We don't care about order in this case, although the results
			// are ordered anyway, by virtue of the way we do grouping.  But
			// if that behavior ever changes, we don't need to care.
			wantUnordered: true,
			want: []rowResult{
				{Key: `"d"`, Value: `1`},
				{Key: `"a"`, Value: `1`},
				{Key: `"b"`, Value: `1`},
				{Key: `"c"`, Value: `1`},
			},
		}
	})

	/*
		TODO:
		- _stats
			- differing lengths of arrays of floats
			- array with floats and other types
			- array with floats and nulls
			- non-integer count in array
			- negative sumsqr in array (??)
			- arrays of stats
			- differing lengths of arrays of stats
			- invalid value to _stats renders non-reduced view broken as well
		- _sum
			- extended capabilities: https://docs.couchdb.org/en/stable/ddocs/ddocs.html#sum
		- built-in reduce functions:
			- _approx_count_distinct (https://docs.couchdb.org/en/stable/ddocs/ddocs.html#approx_count_distinct)
				- _approx_count_distinct
				- start/end keys
				- group behavior
			- _stats (https://docs.couchdb.org/en/stable/ddocs/ddocs.html#stats)
		- map/reduce function takes too long
		- include design docs
		- treat map non-exception errors as exceptions
		- make sure local docs are properly skipped
		- make sure deleted docs are properly skipped
	*/

	tests.Run(t, func(t *testing.T, tt test) {
		t.Parallel()
		db := tt.db
		if db == nil {
			db = newDB(t)
		}
		opts := tt.options
		if opts == nil {
			opts = mock.NilOption
		}
		rows, err := db.Query(context.Background(), tt.ddoc, tt.view, opts)
		if !testy.ErrorMatchesRE(tt.wantErr, err) {
			t.Errorf("Unexpected error: %s", err)
		}
		if status := kivik.HTTPStatus(err); status != tt.wantStatus {
			t.Errorf("Unexpected status: %d", status)
		}
		if err != nil {
			return
		}

		if tt.wantUnordered {
			checkUnorderedRows(t, rows, tt.want)
		} else {
			checkRows(t, rows, tt.want)
		}
		db.checkLogs(tt.wantLogs)
	})
}

func TestDBQuery_reduce_with_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map":    `function(doc) { emit(doc._id, null); }`,
				"reduce": "_count",
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("update_seq", true))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := "2"
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_reduce_without_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map":    `function(doc) { emit(doc._id, null); }`,
				"reduce": "_count",
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("update_seq", false))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := ""
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map": `function(doc) { emit(doc._id, null); }`,
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("update_seq", true))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := "2"
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_no_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map": `function(doc) { emit(doc._id, null); }`,
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", mock.NilOption)
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := ""
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_group_with_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map":    `function(doc) { emit(doc._id, null); }`,
				"reduce": "_count",
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Params(map[string]interface{}{
		"update_seq": true,
		"group":      true,
	}))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := "2"
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_group_without_update_seq(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map":    `function(doc) { emit(doc._id, null); }`,
				"reduce": "_count",
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("group", true))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	want := ""
	got := rows.UpdateSeq()
	if got != want {
		t.Errorf("Unexpected update seq: %s", got)
	}
	_ = rows.Close()
}

func TestDBQuery_update_lazy(t *testing.T) {
	t.Parallel()
	d := newDB(t)

	_ = d.tPut("_design/foo", map[string]interface{}{
		"views": map[string]interface{}{
			"bar": map[string]string{
				"map": `function(doc) { emit(doc._id, null); }`,
			},
		},
	})
	_ = d.tPut("foo", map[string]string{"_id": "foo"})

	// Separate func for defer, to ensure it runs after the query.
	func() {
		// Do a query, with update=lazy, which should return nothing, but trigger
		// an index build.
		rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("update", "lazy"))
		if err != nil {
			t.Fatalf("Failed to query view: %s", err)
		}
		defer rows.Close()
		checkRows(t, rows, nil)
	}()

	// Now wait a moment, and query again to see if there are any results
	time.Sleep(500 * time.Millisecond)

	rows, err := d.Query(context.Background(), "_design/foo", "_view/bar", kivik.Param("update", "false"))
	if err != nil {
		t.Fatalf("Failed to query view: %s", err)
	}
	defer rows.Close()
	checkRows(t, rows, []rowResult{
		{ID: "foo", Key: `"foo"`, Value: "null"},
	})
}
