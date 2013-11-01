// Gocharm processes one or more Juju charms with hooks written in Go.
// All hooks are compiled into a single Go executable, bin/runhook, implemented by
// the runhook package, which must be implemented in the src/runhook
// directory inside the charm. It ignores charms without that directory.
// Note that it compiles the Go executable in cross-compilation mode,
// so cgo-based packages will not work and the resulting charm will
// only work on linux-amd64-based images.
//
// Gocharm increments the revision number of any charms that it
// compiles.
//
// The runhook package must implement a RegisterHooks function which
// must register any hooks required by calling hook.RegisterHook (see
// launchpad.net/juju-utils/hook).
//
// Gocharm runs runhook.RegisterHooks locally to find out what hooks are
// registered, and automatically writes stubs in the hooks directory.
// When the charm is deployed, these will call the runhook executable
// and arrange for registered hook functions to be called. It takes care
// not to overwrite any hooks that may contain custom user changes - it
// might be necessary to remove or change these by hand if gocharm
// prints a warning message about this.
//
// The runhook package is compiled with the charm directory inserted
// before the start of GOPATH, meaning that charm-specific packages can
// be defined and used from runhook.
//
// Currently gocharm iterates through all charms inside $JUJU_REPOSITORY.
//
// TODO change this to allow specific charms to be specified, defaulting
// to the charm enclosing the current directory.
//
// TODO allow a mode that does not compile locally, installing golang
// on the remote node and compiling the code there.
//
// TODO add -clean flag.
//
// TODO use godeps to freeze dependencies into the charm.
//
// TODO examples.
//
// TODO validate metadata against actual registered hooks.
// If there's a hook registered against a relation that's
// not declared, or there's a hook declared but no hooks are
// registered for it, return an error.
//
// TODO(maybe) allow code to register relations, and either
// validate against charm metadata or actually modify the
// charm metadata in place (would require a charm.WriteMeta
// function and users might not like that, as it may mess up formatting)
// package hook; func (r *Registry) RegisterRelation(name string, rel charm.Relation)
//
// TODO allow install and start hooks to be omitted if desired - generate them
// automatically if necessary.
package main

import (
	"flag"
	"fmt"
	"github.com/kr/fs"
	"io/ioutil"
	"launchpad.net/errgo/errors"
	"launchpad.net/juju-core/charm"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var repo = flag.String("repo", "", "charm repo directory (defaults to JUJU_REPOSITORY)")
var test = flag.Bool("test", false, "run tests before building")
var verbose = flag.Bool("v", false, "print information about charms being built")

var exitCode = 0

func main() {
	flag.Parse()
	// TODO accept charm name arguments on the command line
	// to restrict the build to those charms only.
	if *repo == "" {
		if *repo = os.Getenv("JUJU_REPOSITORY"); *repo == "" {
			fatalf("no charm repo directory specified")
		}
	}
	paths, _ := filepath.Glob(filepath.Join(*repo, "*", "*", "metadata.yaml"))
	if len(paths) == 0 {
		fatalf("no charms found")
	}
	var dirs []*charm.Dir
	for _, path := range paths {
		path := filepath.Dir(path)
		dir, err := charm.ReadDir(path)
		if err != nil {
			errorf("cannot read %q: %v", path, err)
			continue
		}
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		isGo, err := isGoCharm(dir)
		if err != nil {
			warningf("cannot determine if %q is a Go charm: %v", dir.Path, err)
			continue
		}
		if !isGo {
			if *verbose {
				log.Printf("ignoring non-Go charm %s", dir.Path)
			}
			continue
		}
		if *verbose {
			log.Printf("processing %v", dir.Path)
		}
		doneSomething, err := processGoCharm(dir)
		if err != nil {
			log.Printf("error info: %s", errors.Info(err))
			errorf("failed compile or test charm %q: %v", dir.Path, err)
			continue
		}
		if doneSomething {
			if err := dir.SetDiskRevision(dir.Revision() + 1); err != nil {
				errorf("cannot bump revision on %q: %v", dir.Path, err)
			}
			_, series := filepath.Split(filepath.Dir(dir.Path))
			fmt.Printf("local:%s/%s-%d\n", series, dir.Meta().Name, dir.Revision())
		}
	}
	os.Exit(exitCode)
}

const hookMainCode = `
// This file is automatically generated. Do not edit.

package main
import (
	"fmt"
	runhook "runhook"
	"launchpad.net/juju-utils/hook"
	"os"
)

func main() {
	r := hook.NewRegistry()
	runhook.RegisterHooks(r)
	if err := hook.Main(r); err != nil {
		fmt.Fprintf(os.Stderr, "runhook: %v\n", err)
		os.Exit(1)
	}
}
`

func processGoCharm(dir *charm.Dir) (doneSomething bool, err error) {
	defer os.RemoveAll(filepath.Join(dir.Path, "pkg"))
	if *test {
		return false, errors.Wrap(testCharm(dir))
	}
	if err := compile(dir, "runhook", hookMainCode, true); err != nil {
		return false, errors.Wrapf(err, "cannot build hooks main package")
	}
	if _, err := os.Stat(filepath.Join(dir.Path, "bin", "runhook")); err != nil {
		return false, errors.New("runhook command not built")
	}
	hooks, err := registeredHooks(dir)
	if err != nil {
		return false, errors.Wrap(err)
	}
	// We always want to generate a stop hook.
	hooks["stop"] = true
	if err := writeHooks(dir, hooks); err != nil {
		return false, errors.Wrapf(err, "cannot write hooks to charm")
	}
	return true, nil

}

func isGoCharm(dir *charm.Dir) (bool, error) {
	info, err := os.Stat(filepath.Join(dir.Path, "src/runhook"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrap(err)
	}
	if !info.IsDir() {
		return false, nil
	}
	return true, nil
}

func setenv(env []string, entry string) []string {
	i := strings.Index(entry, "=")
	if i == -1 {
		panic("no = in environment entry")
	}
	prefix := entry[0 : i+1]
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = entry
			return env
		}
	}
	return append(env, entry)
}

func compile(dir *charm.Dir, binaryName string, mainCode string, crossCompile bool) error {
	env := setenv(os.Environ(),
		fmt.Sprintf("GOPATH=%s:%s", dir.Path, os.Getenv("GOPATH")),
	)
	if crossCompile {
		env = setenv(env, "CGOENABLED=false")
		env = setenv(env, "GOARCH=amd64")
		env = setenv(env, "GOOS=linux")
	}
	mainDir := filepath.Join(dir.Path, "src", "_main", binaryName)
	if err := os.MkdirAll(mainDir, 0777); err != nil {
		return errors.Wrap(err)
	}
	if err := ioutil.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainCode), 0666); err != nil {
		return errors.Wrap(err)
	}
	c := exec.Command("go", "build", "-o", filepath.Join(dir.Path, "bin", binaryName), "_main/"+binaryName)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = env
	if err := c.Run(); err != nil {
		return errors.Wrapf(err, "failed to build")
	}
	return nil
}

func testCharm(dir *charm.Dir) error {
	pkgs, err := packagesInDir(dir.Path)
	if err != nil {
		return errors.Wrap(err)
	}
	env := append(os.Environ(),
		fmt.Sprintf("GOPATH=%s:%s:%s", dir.Path, os.Getenv("GOPATH")),
	)
	args := make([]string, 0, len(pkgs)+1)
	args = append(args, "test")
	args = append(args, pkgs...)
	if err := run(env, "go", args...); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func packagesInDir(dir string) ([]string, error) {
	pkgs := make(map[string]bool)
	w := fs.Walk(dir)
	srcPrefix := filepath.Join(dir, "src")
	for w.Step() {
		if err := w.Err(); err != nil {
			return nil, errors.Wrap(err)
		}
		if p := w.Path(); strings.HasSuffix(p, ".go") && strings.HasPrefix(p, srcPrefix) {
			parent, _ := filepath.Split(p)
			pkgs[strings.TrimPrefix(parent, srcPrefix)] = true
		}
	}
	var all []string
	for path := range pkgs {
		all = append(all, path)
	}
	sort.Strings(all)
	return all, nil
}

func run(env []string, cmd string, args ...string) error {
	c := exec.Command("go", "test", "./...")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = env
	return errors.Wrap(c.Run())
}

func warningf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "gocharm: warning: %s\n", fmt.Sprintf(f, a...))
}

func errorf(f string, a ...interface{}) {
	exitCode = 1
	fmt.Fprintf(os.Stderr, "gocharm: %s\n", fmt.Sprintf(f, a...))
}

func fatalf(f string, a ...interface{}) {
	errorf(f, a...)
	os.Exit(2)
}
