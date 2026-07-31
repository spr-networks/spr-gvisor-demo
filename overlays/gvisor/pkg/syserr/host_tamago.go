//go:build tamago

package syserr

import "golang.org/x/sys/unix"

const maxErrno = 134

var linuxHostTranslations [maxErrno]*Error

func addHostTranslation(host unix.Errno, trans *Error) {
	if int(host) < len(linuxHostTranslations) {
		linuxHostTranslations[host] = trans
	}
}

func getHostTranslation(err unix.Errno) *Error {
	if int(err) < len(linuxHostTranslations) {
		return linuxHostTranslations[err]
	}
	return nil
}
