package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

var IgnoreTopLevelPath = []string{
	".notmuch",
}

func topLevelIgnore(topLevel, name string) bool {
	relativeName, _ := filepath.Rel(topLevel, name)
	for _, p := range IgnoreTopLevelPath {
		if relativeName == p {
			return true
		}
	}
	return false
}

type listeners struct {
	baseDir    string
	cmd        string
	top        *fsnotify.Watcher
	dirs       *fsnotify.Watcher
	boxChanged chan string
}

func extractBoxName(left, right string) string {
	relativeName, _ := filepath.Rel(left, right)
	return filepath.Dir(filepath.Dir(relativeName))
}

func (l *listeners) createSubListeners() {
	l.dirs, _ = fsnotify.NewWatcher()

	go func() {
		for {
			select {
			case event, ok := <-l.dirs.Events:
				if ok {
					l.boxChanged <- extractBoxName(l.baseDir, event.Name)
					//					log.Printf("box changed: '%s' because of: %s\n", extractBoxName(l.baseDir, event.Name), event.Name)
				}
			case <-l.dirs.Errors:
			}
		}
	}()

	files, _ := ioutil.ReadDir(l.baseDir)
	for _, f := range files {
		if f.IsDir() {
			l.dirs.Add(filepath.Join(l.baseDir, f.Name(), "cur"))
			l.dirs.Add(filepath.Join(l.baseDir, f.Name(), "new"))
			l.dirs.Add(filepath.Join(l.baseDir, f.Name(), "tmp"))
		}
	}
}

func (l *listeners) topLevelChanged() {
	l.dirs.Close()
	l.createSubListeners()
}

func (l *listeners) close() {
	l.top.Close()
	l.dirs.Close()
	close(l.boxChanged)
}

func (l *listeners) createTop() {
	l.top, _ = fsnotify.NewWatcher()

	go func() {
		for {
			select {
			case event, _ := <-l.top.Events:
				if !topLevelIgnore(l.baseDir, event.Name) {
					l.topLevelChanged()
				}
			case <-l.top.Errors:
			}
		}
	}()

	l.top.Add(l.baseDir)
}

func arbitrary(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}

func (l *listeners) triggerUpdateFor(box string) {
	cmd := exec.Command(l.cmd, box)
	cmd.Stdout = os.Stdout
	e := cmd.Run()
	if e != nil {
		fmt.Printf("  error when running command: %v\n", e)
	}
}

func (l *listeners) createBoxChangedListener() {
	l.boxChanged = make(chan string, 10000)
	boxUpdate := make(chan string)
	go func() {
		for {
			l.triggerUpdateFor(<-boxUpdate)
		}
	}()

	go func() {
		boxes := make(map[string]bool)
		for {
			select {
			case newBox := <-l.boxChanged:
				boxes[newBox] = true
			case <-time.After(3 * time.Second):
				if len(boxes) > 0 {
					b := arbitrary(boxes)
					delete(boxes, b)
					boxUpdate <- b
				}
			}
		}
	}()
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("usage: fswatch <maildir base> <cmd>\n")
		os.Exit(1)
	}

	l := new(listeners)
	l.baseDir = os.Args[1]
	l.cmd = os.Args[2]
	l.createBoxChangedListener()
	l.createTop()
	l.createSubListeners()
	defer l.close()
	done := make(chan bool)
	<-done
}
