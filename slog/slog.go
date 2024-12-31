package slog

import (
	"syscall"

	"github.com/alaisi/syscalltodo/io"
	"github.com/alaisi/syscalltodo/str"
)

type level int

const (
	debug level = iota
	info
	err
)

var levelNames = map[level]string{
	debug: "DEBUG",
	info:  "INFO",
	err:   "ERROR",
}

func Debug(msg string) {
	log(debug, msg)
}
func Info(msg string) {
	log(info, msg)
}
func Error(msg string) {
	log(err, msg)
}

func log(level level, msg string) {
	line := str.Ltoa(currentTimeMillis()) + " " +
		levelNames[level] + " " +
		msg + "\n"
	io.Write(2, []byte(line))
}

func currentTimeMillis() int64 {
	tv := syscall.Timeval{}
	if err := syscall.Gettimeofday(&tv); err != nil {
		return -1
	}
	return int64(tv.Sec)*1000 + int64(tv.Usec)/1000
}
