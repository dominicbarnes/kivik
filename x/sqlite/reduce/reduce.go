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

// Package reduce implements CouchDB reduce function handling.
package reduce

import (
	"slices"
)

// Row represents a single row of data to be reduced, or the result of a
// reduction. Key and Value are expected to represent JSON serializable data,
// and passing non-serializable data may result in a panic. ID is only used for
// input rows as returned by a map function. It is always empty for output rows.
type Row struct {
	// First and Last reference the key's primary key, and are used to
	// disambiguate rows with the same key. For map inputs, they should be
	// the same.  For reduced inputs, they represent a range of keys.
	First int
	Last  int
	ID    string
	Key   any
	Value any
}

// Func is the signature of a [CouchDB reduce function], translated to Go.
//
// [CouchDB reduce function]: https://docs.couchdb.org/en/stable/ddocs/ddocs.html#reduce-and-rereduce-functions
type Func func(keys [][2]interface{}, values []interface{}, rereduce bool) ([]interface{}, error)

// Reduce calls fn on rows, and returns the results.
//
// The Key field of the returned row(s) will be set only when grouping.
//
// groupLevel controls grouping.  Possible values:
//
//	-1: Maximum grouping, same as group=true
//	 0: No grouping, same as group=false
//	1+: Group by the first N elements of the key, same as group_level=N
func Reduce(rows []Row, fn Func, groupLevel int) ([]Row, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]Row, 0, 1)
	var first, last int

	callReduce := func(keys [][2]interface{}, values []interface{}, rereduce bool, key []any) error {
		if len(keys) == 0 {
			return nil
		}
		results, err := fn(keys, values, rereduce)
		if err != nil {
			return err
		}
		for _, result := range results {
			row := Row{
				Value: result,
				First: first,
				Last:  last,
			}
			if len(key) > 0 {
				row.Key = key
			}
			out = append(out, row)
			first, last = 0, 0
		}
		return nil
	}

	keys := make([][2]interface{}, 0, len(rows))
	values := make([]interface{}, 0, len(rows))
	var targetKey []any
	var rereduce bool
	for _, row := range rows {
		if groupLevel != 0 {
			switch {
			case targetKey != nil && (!slices.Equal(targetKey, truncateKey(row.Key, groupLevel)) || rereduce != (row.ID == "")):
				if err := callReduce(keys, values, rereduce, targetKey); err != nil {
					return nil, err
				}

				keys = keys[:0]
				values = values[:0]
				fallthrough
			case targetKey == nil:
				targetKey = truncateKey(row.Key, groupLevel)
				rereduce = row.ID == ""
			}
		}
		if first == 0 {
			first = row.First
		}
		last = row.Last

		keys = append(keys, [2]interface{}{row.Key, row.ID})
		values = append(values, row.Value)
	}

	if err := callReduce(keys, values, false, targetKey); err != nil {
		return nil, err
	}

	if len(out) <= 1 {
		// One or fewer results can't have duplicates that need to be re-reduced.
		return out, nil
	}

	// If we received mixed map/reduce inputs, then we may need to re-reduce
	// the output before returning.
	lastKey := truncateKey(out[0].Key, groupLevel)
	for i := 1; i < len(out); i++ {
		key := truncateKey(out[i].Key, groupLevel)
		if slices.Equal(lastKey, key) {
			return Reduce(out, fn, groupLevel)
		}
	}

	return out, nil
}

// truncateKey truncates the key to the given level.
func truncateKey(key any, level int) []any {
	if level == 0 {
		return nil
	}
	var target []any
	if tk, ok := key.([]any); ok {
		target = tk
	} else {
		target = []any{key}
	}
	if level > 0 && level < len(target) {
		target = target[:level]
	}
	return target
}

/*
	- First group inputs by key according to group level

*/
