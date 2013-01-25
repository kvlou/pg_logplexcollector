// A serve data base is used by pg_logplexcollector to tell it what
// sockets to listen on, what identification to expect on that socket,
// and how to map the client's self-described identity with a private
// logplex token defined in the serve file.
//
// Of particular importance is being able to learn new serve records
// when a new file is provided at run-time, and ideally not crash the
// server if an incorrect serve file is encountered.
//
// To this end, the serve database is a directory that may look like
// this:
//
//     servedb
//     ├── last_error
//     ├── serves.loaded
//     ├── serves.new
//     └── serves.rej
//
// The general idea is that another program may rename() (for
// atomicity) a new serve file into serves.new.  Subsequently, any
// point, pg_logplexcollector may elect to read this file and, should
// it be found valid, adhere to its new directives and write out a
// *copy* to serves.loaded, which may be monitored by any other
// program on a read-only basis.  After this copy is complete,
// serves.new, any existing serves.rej, and last_error is unlinked.
//
// However, should pg_logplexcollector find the serves.new file to be
// invalid, it will write an error message to a newly created
// last_error file and rename() the file to serves.rej.
//
// The intention of copying the file when it is valid and renaming it
// when it is not is so that it's much harder to write an accidentally
// incorrect program with a dangling file handle to serves.new to
// corrupt serves.loaded, causing confusion.  The intention of using
// rename() to move serves.new to serves.rej is to allow external
// programs to easily determine if a change has been accepted or
// rejected by the use of stat() information.
//
// serves.new must have at least the following structure:
//
//     {"serves": [
//	    {"i": "identity1": "t": "serve1", "p": "/var/run/cluster1/log.sock"},
//	    {"i": "identity2": "t": "serve2", "p": "/var/run/cluster2/log.sock"}
//	 ]
//     }
//
// Any other auxiliary keys and values as siblings to the "serves" key
// are acceptable, and recommended for use for bookkeeping in other
// programs.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
)

type sKey struct {
	I string
	P string
}

type serveRecord struct {
	sKey
	T string
}

type serveDb struct {
	path string

	// For safety under concurrent access
	accessProtect sync.RWMutex

	identToServe map[sKey]*serveRecord

	// To control semantics of first Poll(), which may load
	// serves.loaded from a cold start.
	beyondFirstTime bool
}

// Return value for complex multiple-error cases, as there are code
// paths here where error reporting itself can have errors.  Since
// cases where this is thought to happen are signs that things have
// seriously gone wrong, be assiduous in reporting as much information
// as possible.
type multiError struct {
	error
	nested error
}

func newServeDb(path string) *serveDb {
	return &serveDb{
		path:         path,
		identToServe: make(map[sKey]*serveRecord),
	}
}

func (t *serveDb) loadedPath() string {
	return path.Join(t.path, "serves.loaded")
}

func (t *serveDb) newPath() string {
	return path.Join(t.path, "serves.new")
}

func (t *serveDb) rejPath() string {
	return path.Join(t.path, "serves.rej")
}

func (t *serveDb) errPath() string {
	return path.Join(t.path, "last_error")
}

func (t *serveDb) Resolve(k sKey) (*serveRecord, bool) {
	t.accessProtect.RLock()
	defer t.accessProtect.RUnlock()

	rec, ok := t.identToServe[k]

	return rec, ok
}

func (t *serveDb) protWrite(newMap map[sKey]*serveRecord) {
	t.accessProtect.Lock()
	defer t.accessProtect.Unlock()

	t.identToServe = newMap
}

func (t *serveDb) pollFirstTime() error {
	lp := t.loadedPath()
	contents, err := ioutil.ReadFile(lp)
	if err != nil {
		if os.IsNotExist(err) {
			// old serves.loaded doesn't exist: that's
			// okay; it's just a fresh database.
			return nil
		} else {
			return err
		}
	}

	newMapping, err := t.parse(contents)
	if err != nil {
		// The old 'loaded' mapping is thought to have been
		// good, exit early if that is not true.
		return err
	}

	t.protWrite(newMapping)

	return nil
}

// Poll for new routing information to load
func (t *serveDb) Poll() (err error) {
	// Handle first execution on creation of the db instance.
	if !t.beyondFirstTime {
		if err = t.pollFirstTime(); err != nil {
			return err
		}

		t.beyondFirstTime = true
	}

	p := t.newPath()
	contents, err := ioutil.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// This is the most common branch, where no
			// serves.new file has been provided for
			// loading.  Being that, silence the error.
			return nil
		}

		// Had some problems reading an existing file.
		return err
	}

	// Validate that the JSON is in the expected format.
	newMapping, nonfatale := t.parse(contents)
	if nonfatale != nil {
		// Nope, can't understand the passed JSON, reject it.
		if err := t.reject(p, nonfatale); err != nil {
			return multiError{error: err, nested: nonfatale}
		}

		// Rejection went okay: that's not considered an error
		// for the caller, because it's likely the caller will
		// want to do something extreme in event of Poll()
		// errors, which otherwise tend to arise from serious
		// conditions preventing data base manipulation like
		// "out of disk".
		return nil
	}

	// The new serve mapping was loaded successfully: before
	// installing it reflect its state in the data base first, so
	// a crash will yield the new state rather than the old one.
	if err := t.persistLoaded(contents); err != nil {
		return err
	}

	// Remove last_error and serves.rej file as the persistence
	// has gone well.  As these files are somewhat advisory, don't
	// consider it a failure if such removals do not succeed.
	os.Remove(t.errPath())
	os.Remove(t.rejPath())

	// Commit to the new mappings in this session.
	t.protWrite(newMapping)

	return nil
}

// Persist the verified contents, which are presumed valid.
//
// This is done carefully through temporary files and renames for
// reasons of atomicity, and with both file and directory flushing for
// durability.
func (t *serveDb) persistLoaded(contents []byte) (err error) {
	// Get a file descriptor for the directory before doing
	// anything too complex, because it's necessary for this to
	// succeed before being able to process Sync() requests.
	dir, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer dir.Close()

	tempf, err := ioutil.TempFile(t.path, "tmp_")
	renamedOk := false
	if err != nil {
		return err
	}

	// Handle closing the temporary file and nesting errors.
	defer func() {
		// If no rename has taken place, unlink the temp file.
		if !renamedOk {
			if e := os.Remove(tempf.Name()); e != nil {
				if err != nil {
					err = multiError{
						error:  e,
						nested: err,
					}
				}
			}
		}

		// Close the temp file.
		if e := tempf.Close(); e != nil {
			if err != nil {
				err = multiError{error: e, nested: err}
			}
		}
	}()

	// Fill the temp file with the accepted contents
	_, err = tempf.Write(contents)
	if err != nil {
		return err
	}

	err = tempf.Sync()
	if err != nil {
		return err
	}

	// Move the temporary file into place
	err = os.Rename(tempf.Name(), path.Join(t.path, "serves.loaded"))
	if err != nil {
		return err
	}

	// Even though rename is not yet durable, it is visible
	// already.
	renamedOk = true

	// Flush the rename so a crash will not effectively un-do it.
	err = dir.Sync()
	if err != nil {
		return err
	}

	// Purge submitted serve file, as it has been accepted and
	// copied.
	err = os.Remove(t.newPath())
	if err != nil {
		return err
	}

	// Flush to make the removal of the submitted file durable.
	err = dir.Sync()
	if err != nil {
		return err
	}

	return nil
}

func (t *serveDb) reject(submitPath string, nonfatale error) (err error) {
	// Perform move to the rejection file
	err = os.Rename(submitPath, t.rejPath())
	if err != nil {
		return err
	}

	// Render and write the cause of the rejection.  Don't bother
	// syncing it to disk: an incomplete or empty file on a crash
	// seems acceptable for now.
	err = ioutil.WriteFile(
		t.errPath(),
		[]byte(fmt.Sprintf("%#v\n", nonfatale)),
		0400)
	if err != nil {
		return err
	}

	return nil
}

func projectFromJson(v interface{}) (*serveRecord, error) {
	maybeMap, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(
			"expected a JSON map for in the \"serve\" list, "+
				"instead received %v", v)
	}

	lookup := func(key string) (string, error) {
		ms, ok := maybeMap[key]
		if !ok {
			return "", fmt.Errorf("did not receive an expected "+
				"(\"%s\") key in serve record", key)
		}

		s, ok := ms.(string)
		if !ok {
			return "", fmt.Errorf("expected string value for key "+
				"(\"%s\") key in serve record", key)
		}

		return s, nil
	}

	path, err := lookup("p")
	if err != nil {
		return nil, err
	}

	tok, err := lookup("t")
	if err != nil {
		return nil, err
	}

	ident, err := lookup("i")
	if err != nil {
		return nil, err
	}

	return &serveRecord{sKey: sKey{P: path, I: ident},
		T: tok}, nil
}

func (t *serveDb) parse(contents []byte) (map[sKey]*serveRecord, error) {
	filled := make(map[string]interface{})
	filledp := &filled
	err := json.Unmarshal(contents, filledp)
	if err != nil {
		return nil, err
	}

	if filledp == nil {
		return nil, fmt.Errorf(
			"expected JSON dictionary, got JSON null")
	}

	maybeServeValue := filled["serves"]
	maybeList, ok := maybeServeValue.([]interface{})
	if !ok {
		return nil, fmt.Errorf("Expected 'serves' key to contain "+
			"a JSON list, instead it contains %T",
			maybeServeValue)
	}

	// Fill a new mapping, optimistic that the input is correct,
	// but abort if a non-JSON string is found on the
	// right-hand-side of the dictionary, where the serve value
	// ought to be.
	newMapping := make(map[sKey]*serveRecord)
	for _, val := range maybeList {
		rec, err := projectFromJson(val)
		if err != nil {
			return nil, err
		}

		newMapping[rec.sKey] = rec
	}

	return newMapping, nil
}
