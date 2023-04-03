package checks

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChecks_sonameParsePackages(t *testing.T) {
	dir := t.TempDir()

	// packages.log is an example output of packages that were built by melange, eg wolfi/os
	data, err := os.ReadFile(filepath.Join("testdata", "packages.log"))
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// if a top level melange package was built then it's subpackages will also have been built
	melangeData, err := os.ReadFile(filepath.Join("testdata", "subpackages_melange.yaml"))
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	err = os.WriteFile(filepath.Join(dir, "bind.yaml"), melangeData, os.ModePerm)
	assert.NoError(t, err)

	o := &SoNameOptions{}
	o.Dir = dir

	o.PackageListFilename = filepath.Join("testdata", "packages.log")
	packages, err := o.getNewPackages()
	assert.NoError(t, err)

	assert.Equal(t, "3.7.8", packages["gnutls-c++"].Version)
	assert.Equal(t, "1.2.3", packages["bind-doc"].Version)
	assert.Equal(t, "1.2.3", packages["bind-dev"].Version)
	assert.Equal(t, "1.2.3", packages["grape-utils"].Version)
}

func TestChecks_getSonameFiles(t *testing.T) {
	tests := []struct {
		name        string
		sonameFiles []string
		dtSoname    []string
		match       bool
		wantErr     assert.ErrorAssertionFunc
	}{
		{
			name: "match", sonameFiles: []string{
				"foo.so",
				"foo.so.1",
				"foo.so.11",
				"foo.so.1.1",
				"libstdc++.so.6.0.30-gdb.py",
			}, dtSoname: []string{"cheese.so.1.1"}, match: true,
			wantErr: assert.NoError,
		},
		{
			name: "dont_match", sonameFiles: []string{
				"foo",
				"XIDefineCursor.3",
				"README.solaris2",
			}, match: false,
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := SoNameOptions{}

			dir := t.TempDir()

			for _, f := range tt.sonameFiles {
				err := os.WriteFile(filepath.Join(dir, f), []byte("test"), os.ModePerm)
				assert.NoError(t, err)
			}

			// simulate DT_SONAME
			for _, f := range tt.dtSoname {
				err := os.WriteFile(filepath.Join(dir, f), []byte("test"), os.ModePerm)
				assert.NoError(t, err)
				err = os.Link(filepath.Join(dir, f), filepath.Join(dir, "cheese.so.1"))
				assert.NoError(t, err)
			}

			got, err := o.getSonameFiles(dir)
			assert.NoError(t, err)

			expectedCount := 0
			if tt.match {
				expectedCount = len(tt.sonameFiles) + len(tt.dtSoname)
			}

			assert.Equal(t, expectedCount, len(got))
		})
	}
}

func TestChecks_downloadCurrentAPK(t *testing.T) {
	dir := t.TempDir()

	o := SoNameOptions{}

	data, err := os.ReadFile(filepath.Join("testdata", "hello-world-0.0.1-r0.apk"))
	assert.NoError(t, err)

	// create a test server to download the test apk from
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Test request parameters
		assert.Equal(t, req.URL.String(), "/hello-world-0.0.1-r0.apk")

		// Send response to be tested
		_, err = rw.Write(data)
		assert.NoError(t, err)
	}))

	o.ApkIndexURL = server.URL + "/APKINDEX"

	o.Client = server.Client()
	err = o.downloadCurrentAPK("hello-world-0.0.1-r0.apk", dir)
	assert.NoError(t, err)

	data, err = os.ReadFile(filepath.Join(dir, "hello-world-0.0.1-r0.apk"))
	assert.NoError(t, err)

	assert.NotEmpty(t, data)
}

func TestSoNameOptions_checkSonamesMatch(t *testing.T) {
	tests := []struct {
		name                string
		existingSonameFiles []string
		newSonameFiles      []string
		wantErr             assert.ErrorAssertionFunc
	}{
		{
			name: "deleted", existingSonameFiles: []string{"foo.so", "bar.so"}, newSonameFiles: []string{"foo.so"},
			wantErr: assert.NoError,
		},
		{
			name: "match", existingSonameFiles: []string{"foo.so", "bar.so"}, newSonameFiles: []string{"foo.so", "bar.so"},
			wantErr: assert.NoError,
		},
		{
			name: "ignore", existingSonameFiles: []string{"foo.so"}, newSonameFiles: []string{"foo.so.1"},
			wantErr: assert.NoError,
		},
		{
			name: "match", existingSonameFiles: []string{"foo.so.1"}, newSonameFiles: []string{"foo.so.1"},
			wantErr: assert.NoError,
		},
		{
			name: "match_multiple", existingSonameFiles: []string{"foo.so.1", "bar.so.2"}, newSonameFiles: []string{"foo.so.1", "bar.so.2"},
			wantErr: assert.NoError,
		},
		{
			name: "match_multiple_different_order", existingSonameFiles: []string{"bar.so.2", "foo.so.1"}, newSonameFiles: []string{"foo.so.1", "bar.so.2"},
			wantErr: assert.NoError,
		},
		{
			name: "single_fail", existingSonameFiles: []string{"foo.so.1"}, newSonameFiles: []string{"foo.so.2"},
			wantErr: assert.Error,
		},
		{
			name: "multi_fail", existingSonameFiles: []string{"foo.so.1", "bar.so.1"}, newSonameFiles: []string{"foo.so.1", "bar.so.2"},
			wantErr: assert.Error,
		},
		{
			name: "skip_new", existingSonameFiles: []string{"foo.so.1", "bar.so.1"}, newSonameFiles: []string{"cheese.so.1"},
			wantErr: assert.NoError,
		},
		{
			name: "abi_compatible", existingSonameFiles: []string{"foo.so.1.2"}, newSonameFiles: []string{"foo.so.1.3"},
			wantErr: assert.NoError,
		},
		{
			name: "no_existing", existingSonameFiles: []string{}, newSonameFiles: []string{"cheese.so.1"},
			wantErr: assert.NoError,
		},
		{
			name: "none_at_all", existingSonameFiles: []string{}, newSonameFiles: []string{},
			wantErr: assert.NoError,
		},
		{
			name: "complex", existingSonameFiles: []string{"libstdc++.so.6.0.30-gdb.py"}, newSonameFiles: []string{"libstdc++.so.6.0.30-gdb.py"},
			wantErr: assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := SoNameOptions{
				Logger: log.New(log.Writer(), "test: ", log.LstdFlags|log.Lmsgprefix),
			}
			tt.wantErr(t, o.checkSonamesMatch(tt.existingSonameFiles, tt.newSonameFiles), fmt.Sprintf("checkSonamesMatch(%v, %v)", tt.existingSonameFiles, tt.newSonameFiles))
		})
	}
}