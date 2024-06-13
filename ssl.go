// Copyright (C) 2017. See AUTHORS.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package openssl

// #include "shim.h"
import "C"

import (
	"os"
	"unsafe"

	"github.com/mattn/go-pointer"
)

type SSLTLSExtErr int

const (
	SSLTLSExtErrOK           SSLTLSExtErr = C.SSL_TLSEXT_ERR_OK
	SSLTLSExtErrAlertWarning SSLTLSExtErr = C.SSL_TLSEXT_ERR_ALERT_WARNING
	SSLTLSEXTErrAlertFatal   SSLTLSExtErr = C.SSL_TLSEXT_ERR_ALERT_FATAL
	SSLTLSEXTErrNoAck        SSLTLSExtErr = C.SSL_TLSEXT_ERR_NOACK
)

var (
	ssl_idx = C.X_SSL_new_index()
)

//export get_ssl_idx
func get_ssl_idx() C.int {
	return ssl_idx
}

type SSLContentType int

const (
	SSL3_RT_CHANGE_CIPHER_SPEC SSLContentType = C.SSL3_RT_CHANGE_CIPHER_SPEC
	SSL3_RT_ALERT              SSLContentType = C.SSL3_RT_ALERT
	SSL3_RT_HANDSHAKE          SSLContentType = C.SSL3_RT_HANDSHAKE
	SSL3_RT_APPLICATION_DATA   SSLContentType = C.SSL3_RT_APPLICATION_DATA
)

type MessageCallback func(
	ssl *SSL,
	isSending bool,
	protocolVersion Version,
	contentType SSLContentType,
	content []byte,
)

type SSL struct {
	ssl       *C.SSL
	verify_cb VerifyCallback
	message_callback MessageCallback
}

//export go_ssl_verify_cb_thunk
func go_ssl_verify_cb_thunk(p unsafe.Pointer, ok C.int, ctx *C.X509_STORE_CTX) C.int {
	defer func() {
		if err := recover(); err != nil {
			logger.Critf("openssl: verify callback panic'd: %v", err)
			os.Exit(1)
		}
	}()
	verify_cb := pointer.Restore(p).(*SSL).verify_cb
	// set up defaults just in case verify_cb is nil
	if verify_cb != nil {
		store := &CertificateStoreCtx{ctx: ctx}
		if verify_cb(ok == 1, store) {
			ok = 1
		} else {
			ok = 0
		}
	}
	return ok
}

//export go_ssl_msg_cb_thunk
func go_ssl_message_callback_thunk(
	goSslStruct unsafe.Pointer,
	write_p,
	version,
	content_type C.int,
	buffer unsafe.Pointer,
	bufferLength C.int,
) {
	defer func() {
		if err := recover(); err != nil {
			logger.Critf("openssl: message callback panic'd: %v", err)
			os.Exit(1)
		}
	}()

	ssl := pointer.Restore(goSslStruct).(*SSL)
	if ssl.message_callback == nil {
		return
	}

	ssl.message_callback(
		ssl,
		write_p == 1,
		Version(version),
		SSLContentType(content_type),
		// C.GoBytes creates a copy (https://pkg.go.dev/cmd/cgo) and the original
		// buffer is freed by openssl:
		// "The buffer is no longer valid after the callback function has returned"
		C.GoBytes(buffer, bufferLength),
	)
}

// Wrapper around SSL_get_servername. Returns server name according to rfc6066
// http://tools.ietf.org/html/rfc6066.
func (s *SSL) GetServername() string {
	return C.GoString(C.SSL_get_servername(s.ssl, C.TLSEXT_NAMETYPE_host_name))
}

// GetOptions returns SSL options. See
// https://www.openssl.org/docs/ssl/SSL_CTX_set_options.html
func (s *SSL) GetOptions() Options {
	return Options(C.X_SSL_get_options(s.ssl))
}

// SetOptions sets SSL options. See
// https://www.openssl.org/docs/ssl/SSL_CTX_set_options.html
func (s *SSL) SetOptions(options Options) Options {
	return Options(C.X_SSL_set_options(s.ssl, C.long(options)))
}

// ClearOptions clear SSL options. See
// https://www.openssl.org/docs/ssl/SSL_CTX_set_options.html
func (s *SSL) ClearOptions(options Options) Options {
	return Options(C.X_SSL_clear_options(s.ssl, C.long(options)))
}

// SetMsgCallback sets callback that's executed on every TLS message
// sent or received. Note that version & content_type may contain invalid
// values, see the openssl docs. Additionally, we chose to omit anything
// related to SSL_set_msg_callback_arg as it can be easily replicated with
// Golangs closures.
// https://www.openssl.org/docs/man1.1.1/man3/SSL_CTX_set_msg_callback.html
func (s *SSL) SetMsgCallback(callback MessageCallback) {
	s.message_callback = callback
	if s.message_callback != nil {
		C.SSL_set_msg_callback(s.ssl, (*[0]byte)(C.X_SSL_message_callback))
	} else {
		C.SSL_set_msg_callback(s.ssl, nil)
	}
}

// SetVerify controls peer verification settings. See
// http://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) SetVerify(options VerifyOptions, verify_cb VerifyCallback) {
	s.verify_cb = verify_cb
	if verify_cb != nil {
		C.SSL_set_verify(s.ssl, C.int(options), (*[0]byte)(C.X_SSL_verify_cb))
	} else {
		C.SSL_set_verify(s.ssl, C.int(options), nil)
	}
}

// SetVerifyMode controls peer verification setting. See
// http://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) SetVerifyMode(options VerifyOptions) {
	s.SetVerify(options, s.verify_cb)
}

// SetVerifyCallback controls peer verification setting. See
// http://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) SetVerifyCallback(verify_cb VerifyCallback) {
	s.SetVerify(s.VerifyMode(), verify_cb)
}

// GetVerifyCallback returns callback function. See
// http://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) GetVerifyCallback() VerifyCallback {
	return s.verify_cb
}

// VerifyMode returns peer verification setting. See
// http://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) VerifyMode() VerifyOptions {
	return VerifyOptions(C.SSL_get_verify_mode(s.ssl))
}

// SetVerifyDepth controls how many certificates deep the certificate
// verification logic is willing to follow a certificate chain. See
// https://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) SetVerifyDepth(depth int) {
	C.SSL_set_verify_depth(s.ssl, C.int(depth))
}

// GetVerifyDepth controls how many certificates deep the certificate
// verification logic is willing to follow a certificate chain. See
// https://www.openssl.org/docs/ssl/SSL_CTX_set_verify.html
func (s *SSL) GetVerifyDepth() int {
	return int(C.SSL_get_verify_depth(s.ssl))
}

// SetSSLCtx changes context to new one. Useful for Server Name Indication (SNI)
// rfc6066 http://tools.ietf.org/html/rfc6066. See
// http://stackoverflow.com/questions/22373332/serving-multiple-domains-in-one-box-with-sni
func (s *SSL) SetSSLCtx(ctx *Ctx) {
	/*
	 * SSL_set_SSL_CTX() only changes certs as of 1.0.0d
	 * adjust other things we care about
	 */
	C.SSL_set_SSL_CTX(s.ssl, ctx.ctx)
}

// GetVersion() returns the name of the protocol used for the connection. It
// should only be called after the initial handshake has been completed otherwise
// the result may be unreliable.
// https://www.openssl.org/docs/man1.0.2/man3/SSL_get_version.html
func (s *SSL) GetVersion() string {
	return C.GoString(C.SSL_get_version(s.ssl))
}

//export sni_cb_thunk
func sni_cb_thunk(p unsafe.Pointer, con *C.SSL, ad unsafe.Pointer, arg unsafe.Pointer) C.int {
	defer func() {
		if err := recover(); err != nil {
			logger.Critf("openssl: verify callback sni panic'd: %v", err)
			os.Exit(1)
		}
	}()

	sni_cb := pointer.Restore(p).(*Ctx).sni_cb

	s := &SSL{ssl: con}
	// This attaches a pointer to our SSL struct into the SNI callback.
	C.SSL_set_ex_data(s.ssl, get_ssl_idx(), pointer.Save(s))

	// Note: this is ctx.sni_cb, not C.sni_cb
	return C.int(sni_cb(s))
}
