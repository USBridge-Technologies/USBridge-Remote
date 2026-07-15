//go:build ios && !android && cgo

package service

/*
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/src -I${SRCDIR}/../../moonlight-common-c/enet/include
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/build/ios/openssl/include
#cgo CFLAGS: -I${SRCDIR}/../../moonlight-common-c/build/ios/opus/include/opus
#cgo LDFLAGS: -L${SRCDIR}/../../moonlight-common-c/build/ios -lmoonlight-common-c -lenet -lssl -lcrypto -lopus
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreFoundation -framework CoreVideo -framework AudioToolbox -framework CoreAudio -framework QuartzCore -framework UIKit
*/
import "C"
