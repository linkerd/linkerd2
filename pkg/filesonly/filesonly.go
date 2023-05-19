package filesonly

/* An implementation of the http.FileSystem interface that disallows
   listing the contents of directories. This approach is adapted from:
	 https://groups.google.com/d/topic/golang-nuts/bStLPdIVM6w/discussion

	 Source: https://github.com/BuoyantIO/util
*/

import (
	"net/http"
	"os"
)

// FileSystem provides access to a collection of named files via
// http.FileSystem, given a directory.
func FileSystem(dir string) http.FileSystem {
	return fileSystem{http.Dir(dir)}
}

type fileSystem struct {
	fs http.FileSystem
}

func (fs fileSystem) Open(name string) (http.File, error) {
	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return file{f}, nil
}

type file struct {
	http.File
}

func (f file) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}
