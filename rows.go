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

package kivik

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/go-kivik/kivik/v4/driver"
)

// Rows is an iterator over a a multi-value query.
type Rows interface {
	// Next prepares the next result value for reading. It returns true on
	// success or false if there are no more results or an error occurs while
	// preparing it. Err should be consulted to distinguish between the two.
	Next() bool

	// Err returns the error, if any, that was encountered during iteration.
	// Err may be called after an explicit or implicit Close.
	Err() error

	// Close closes the Rows, preventing further enumeration, and freeing any
	// resources (such as the http request body) of the underlying query. If
	// Next is called and there are no further results, Rows is closed
	// automatically and it will suffice to check the result of Err. Close is
	// idempotent and does not affect the result of Err.
	Close() error

	// ScanValue copies the data from the result value into the value pointed
	// at by dest. Think of this as a json.Unmarshal into dest.
	//
	// If the dest argument has type *[]byte, Scan stores a copy of the input
	// data. The copy is owned by the caller and can be modified and held
	// indefinitely.
	//
	// The copy can be avoided by using an argument of type *json.RawMessage
	// instead. After a ScanValue into a json.RawMessage, the slice is only
	// valid until the next call to Next or Close.
	//
	// For all other types, refer to the documentation for json.Unmarshal for
	// type conversion rules.
	ScanValue(dest interface{}) error

	// ScanDoc works the same as ScanValue, but on the doc field of the result.
	// It will return an error if the query does not include documents.
	ScanDoc(dest interface{}) error

	// ScanAllDocs loops through remaining documents in the iterator, and scans
	// them into dest. Dest is expected to be a pointer to a slice or an array,
	// any other type will return an error. If dest is an array, scanning will
	// stop once the array is filled.  The iterator is closed by this method.
	// It is possible that an error will be returned, and that one or more
	// documents were successfully scanned.
	ScanAllDocs(dest interface{}) error

	// ScanKey works the same as ScanValue, but on the key field of the result.
	// For simple keys, which are just strings, the Key() method may be easier
	// to use.
	ScanKey(dest interface{}) error

	// ID returns the ID of the current result.
	ID() string

	// Key returns the Key of the current result as a raw JSON string. For
	// compound keys, the ScanKey() method may be more convenient.
	Key() string

	// Offset returns the starting offset where the result set started. It is
	// only guaranteed to be set after all result rows have been enumerated
	// through by Next, and thus should only be read after processing all rows
	// in a result set. Calling Close before enumerating will render this value
	// unreliable.
	Offset() int64

	// TotalRows returns the total number of rows in the view which would have
	// been returned if no limiting were used. This value is only guaranteed to
	// be set after all result rows have been enumerated through by Next, and
	// thus should only be read after processing all rows in a result set.
	// Calling Close before enumerating will render this value unreliable.
	TotalRows() int64

	// UpdateSeq returns the sequence id of the underlying database the view
	// reflects, if requested in the query. This value is only guaranteed to be
	// set after all result rows have been enumerated through by Next, and thus
	// should only be read after processing all rows in a result set. Calling
	// Close before enumerating will render this value unreliable.
	UpdateSeq() string

	// Warning returns a warning generated by the query, if any. This value is
	// only guaranteed to be set after all result rows have been enumeratd
	// through by Next.
	Warning() string

	// QueryIndex returns the 0-based index of the query. For standard queries,
	// this is always 0. When multiple queries are passed to the view, this will
	// represent the query currently being iterated
	QueryIndex() int

	// Bookmark returns the paging bookmark, if one was provided with the result
	// set. This is intended for use with the Mango /_find interface, with
	// CouchDB 2.1.1 and later. Consult the official CouchDB documentation for
	// detailed usage instructions:
	// http://docs.couchdb.org/en/2.1.1/api/database/find.html#pagination
	Bookmark() string

	// EOQ returns true if the iterator has reached the end of a query in a
	// multi-query query. When EOQ is true, the row data will not have been
	// updated. It is common to simply `continue` in case of EOQ, unless you
	// care about the per-query metadata, such as offset, total rows, etc.
	EOQ() bool
}

type rows struct {
	*iter
	rowsi driver.Rows
	err   error
}

var _ Rows = &rows{}

func (r *rows) Next() bool {
	return r.iter.Next()
}

func (r *rows) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Err()
}

func (r *rows) Close() error {
	return r.iter.Close()
}

type rowsIterator struct{ driver.Rows }

var _ iterator = &rowsIterator{}

func (r *rowsIterator) Next(i interface{}) error { return r.Rows.Next(i.(*driver.Row)) }

func newRows(ctx context.Context, rowsi driver.Rows) *rows {
	return &rows{
		iter:  newIterator(ctx, &rowsIterator{rowsi}, &driver.Row{}),
		rowsi: rowsi,
	}
}

func (r *rows) ScanValue(dest interface{}) error {
	if r.err != nil {
		return r.err
	}
	runlock, err := r.rlock()
	if err != nil {
		return err
	}
	defer runlock()
	row := r.curVal.(*driver.Row)
	if row.Error != nil {
		return row.Error
	}
	if row.ValueReader != nil {
		return json.NewDecoder(row.ValueReader).Decode(dest)
	}
	return json.Unmarshal(row.Value, dest)
}

func (r *rows) ScanDoc(dest interface{}) error {
	if r.err != nil {
		return r.err
	}
	runlock, err := r.rlock()
	if err != nil {
		return err
	}
	defer runlock()
	row := r.curVal.(*driver.Row)
	if err := row.Error; err != nil {
		return err
	}
	doc := row.Doc
	if row.DocReader != nil {
		return json.NewDecoder(row.DocReader).Decode(dest)
	}
	if doc != nil {
		return json.Unmarshal(doc, dest)
	}
	return &Error{HTTPStatus: http.StatusBadRequest, Message: "kivik: doc is nil; does the query include docs?"}
}

func (r *rows) ScanAllDocs(dest interface{}) (err error) {
	defer func() {
		closeErr := r.Close()
		if err == nil {
			err = closeErr
		}
	}()
	if r.err != nil {
		return r.err
	}

	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Ptr {
		return errors.New("must pass a pointer to ScanAllDocs")
	}
	if value.IsNil() {
		return errors.New("nil pointer passed to ScanAllDocs")
	}

	direct := reflect.Indirect(value)
	var limit int

	switch direct.Kind() {
	case reflect.Array:
		limit = direct.Len()
		if limit == 0 {
			return errors.New("0-length array passed to ScanAllDocs")
		}
	case reflect.Slice:
	default:
		return errors.New("dest must be a pointer to a slice or array")
	}

	base := value.Type()
	if base.Kind() == reflect.Ptr {
		base = base.Elem()
	}
	base = base.Elem()

	for i := 0; r.Next(); i++ {
		if limit > 0 && i >= limit {
			return nil
		}
		vp := reflect.New(base)
		err = r.ScanDoc(vp.Interface())
		if limit > 0 { // means this is an array
			direct.Index(i).Set(reflect.Indirect(vp))
		} else {
			direct.Set(reflect.Append(direct, reflect.Indirect(vp)))
		}
	}
	return nil
}

func (r *rows) ScanKey(dest interface{}) error {
	if r.err != nil {
		return r.err
	}
	runlock, err := r.rlock()
	if err != nil {
		return err
	}
	defer runlock()
	row := r.curVal.(*driver.Row)
	if err := row.Error; err != nil {
		return err
	}
	return json.Unmarshal(row.Key, dest)
}

func (r *rows) ID() string {
	runlock, err := r.rlock()
	if err != nil {
		return ""
	}
	defer runlock()
	return r.curVal.(*driver.Row).ID
}

func (r *rows) Key() string {
	runlock, err := r.rlock()
	if err != nil {
		return ""
	}
	defer runlock()
	return string(r.curVal.(*driver.Row).Key)
}

func (r *rows) Offset() int64 {
	return r.rowsi.Offset()
}

func (r *rows) TotalRows() int64 {
	return r.rowsi.TotalRows()
}

func (r *rows) UpdateSeq() string {
	return r.rowsi.UpdateSeq()
}

func (r *rows) Warning() string {
	if w, ok := r.rowsi.(driver.RowsWarner); ok {
		return w.Warning()
	}
	return ""
}

func (r *rows) QueryIndex() int {
	if qi, ok := r.rowsi.(driver.QueryIndexer); ok {
		return qi.QueryIndex()
	}
	return 0
}

func (r *rows) Bookmark() string {
	if b, ok := r.rowsi.(driver.Bookmarker); ok {
		return b.Bookmark()
	}
	return ""
}
