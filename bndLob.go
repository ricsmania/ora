// Copyright 2014 Rana Ian. All rights reserved.
// Use of this source code is governed by The MIT License
// found in the accompanying LICENSE file.

package ora

/*
#include <oci.h>
#include "version.h"
*/
import "C"
import (
	"io"
	"unsafe"
)

type bndLob struct {
	stmt          *Stmt
	ocibnd        *C.OCIBind
	ociLobLocator *C.OCILobLocator
}

// bindReader binds an io.Reader: reads from rdr, and writes to a temprary LOB,
// then binds that.
//
// If Value is nil and Reader is not, then Reader is used.
// The bindReader is a little bit complicated, as only three types of piece
// sequences are allowed:
//
//     a) OCI_ONE_PIECE, one chunk
//     b) OCI_FIRST_PIECE, OCI_LAST_PIECE (two, non-empty chunks)
//     c) OCI_FIRST_PIECE, OCI_NEXT_PIECE*, OCI_LAST_PIECE
//
// None of the chunks can be empty, so we have to pre-read the next chunk,
// before sending the actual, to know whether this is the last or not.
func (bnd *bndLob) bindReader(rdr io.Reader, position int, lobBufferSize int, stmt *Stmt) (err error) {
	bnd.stmt = stmt
	if lobBufferSize <= 0 {
		lobBufferSize = lobChunkSize
	}

	finish, err := bnd.allocTempLob()
	if err != nil {
		return err
	}

	if err = writeLob(bnd.ociLobLocator, bnd.stmt, rdr, lobBufferSize); err != nil {
		bnd.stmt.ses.Break()
		finish()
		return err
	}

	if err = bnd.bindByPos(position); err != nil {
		finish()
		return err
	}
	return nil
}

func (bnd *bndLob) setPtr() error {
	return nil
}

func (bnd *bndLob) freeLob() {
	defer func() {
		recover()
	}()
}

func (bnd *bndLob) close() (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = errR(value)
		}
	}()

	// no need to clear bnd.buf
	// free temporary lob
	C.OCILobFreeTemporary(
		bnd.stmt.ses.ocisvcctx,      //OCISvcCtx          *svchp,
		bnd.stmt.ses.srv.env.ocierr, //OCIError           *errhp,
		bnd.ociLobLocator)           //OCILobLocator      *locp,
	// free lob locator handle
	C.OCIDescriptorFree(
		unsafe.Pointer(bnd.ociLobLocator), //void     *descp,
		C.OCI_DTYPE_LOB)                   //ub4      type );
	stmt := bnd.stmt
	bnd.stmt = nil
	bnd.ocibnd = nil
	bnd.ociLobLocator = nil
	stmt.putBnd(bndIdxLob, bnd)
	return nil
}

func (bnd *bndLob) allocTempLob() (finish func(), err error) {
	bnd.ociLobLocator, finish, err = allocTempLob(bnd.stmt)
	return
}

func (bnd *bndLob) bindByPos(position int) error {
	r := C.OCIBINDBYPOS(
		bnd.stmt.ocistmt,                                //OCIStmt      *stmtp,
		(**C.OCIBind)(&bnd.ocibnd),                      //OCIBind      **bindpp,
		bnd.stmt.ses.srv.env.ocierr,                     //OCIError     *errhp,
		C.ub4(position),                                 //ub4          position,
		unsafe.Pointer(&bnd.ociLobLocator),              //void         *valuep,
		C.LENGTH_TYPE(unsafe.Sizeof(bnd.ociLobLocator)), //sb8          value_sz,
		C.SQLT_BLOB,   //ub2          dty,
		nil,           //void         *indp,
		nil,           //ub2          *alenp,
		nil,           //ub2          *rcodep,
		0,             //ub4          maxarr_len,
		nil,           //ub4          *curelep,
		C.OCI_DEFAULT) //ub4          mode );
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	}

	return nil
}

func writeLob(ociLobLocator *C.OCILobLocator, stmt *Stmt, r io.Reader, lobBufferSize int) error {
	var actBuf, nextBuf []byte
	if lobChunkSize >= lobBufferSize {
		arr := lobChunkPool.Get().([lobChunkSize]byte)
		defer lobChunkPool.Put(arr)
		actBuf = arr[:lobBufferSize]
		arr = lobChunkPool.Get().([lobChunkSize]byte)
		defer lobChunkPool.Put(arr)
		nextBuf = arr[:lobBufferSize]
	} else {
		actBuf = make([]byte, lobBufferSize)
		nextBuf = make([]byte, lobBufferSize)
	}

	// write bytes to lob locator - at once, as we already have all bytes in memory
	var n int
	var byte_amtp, off C.oraub8
	var actPiece, nextPiece C.ub1 = C.OCI_FIRST_PIECE, C.OCI_NEXT_PIECE
	// OCILobWrite2 doesn't support writing zero bytes
	// nor is writing 1 byte and erasing the one byte supported
	// therefore, throw an error
	var err error
	if n, err = io.ReadFull(r, actBuf); err != nil {
		switch err {
		case io.EOF: // no bytes read
			return errNew("writing a zero-length BLOB is unsupported")
		case io.ErrUnexpectedEOF:
			actPiece = C.OCI_ONE_PIECE
		default:
			return err
		}
		actBuf = actBuf[:n]
	}

	for {
		n = len(actBuf)
		if n == lobBufferSize {
			var n2 int
			if n2, err = io.ReadFull(r, nextBuf[:]); err != nil {
				switch err {
				case io.EOF: // no bytes read, lobSize == len(buffer[0])
					if actPiece == C.OCI_FIRST_PIECE {
						actPiece = C.OCI_ONE_PIECE
					} else {
						actPiece = C.OCI_LAST_PIECE
					}
				case io.ErrUnexpectedEOF:
					nextPiece = C.OCI_LAST_PIECE
				default:
					return err
				}
				nextBuf = nextBuf[:n2]
			}
		}

		//Log.Infof("LobWrite2 off=%d len=%d piece=%d", off, n, actPiece)
		byte_amtp = 0
		if actPiece == C.OCI_ONE_PIECE {
			byte_amtp = C.oraub8(n)
		}
		// Write to Oracle
		if C.OCILobWrite2(
			stmt.ses.ocisvcctx,         //OCISvcCtx          *svchp,
			stmt.ses.srv.env.ocierr,    //OCIError           *errhp,
			ociLobLocator,              //OCILobLocator      *locp,
			&byte_amtp,                 //oraub8          *byte_amtp,
			nil,                        //oraub8          *char_amtp,
			off+1,                      //oraub8          offset, starting position is 1
			unsafe.Pointer(&actBuf[0]), //void            *bufp,
			C.oraub8(n),
			actPiece,         //ub1             piece,
			nil,              //void            *ctxp,
			nil,              //OCICallbackLobWrite2 (cbfp)
			C.ub2(0),         //ub2             csid,
			C.SQLCS_IMPLICIT, //ub1             csfrm );
		//fmt.Printf("r %v, current %v, buffer %v\n", r, current, buffer)
		//fmt.Printf("C.OCI_NEED_DATA %v, C.OCI_SUCCESS %v\n", C.OCI_NEED_DATA, C.OCI_SUCCESS)
		) == C.OCI_ERROR {
			return stmt.ses.srv.env.ociError()
		}
		off += byte_amtp

		if actPiece == C.OCI_LAST_PIECE || actPiece == C.OCI_ONE_PIECE {
			break
		}
		actPiece, actBuf = nextPiece, nextBuf
	}
	return nil
}

func allocTempLob(stmt *Stmt) (
	ociLobLocator *C.OCILobLocator,
	finish func(),
	err error,
) {
	// Allocate lob locator handle
	r := C.OCIDescriptorAlloc(
		unsafe.Pointer(stmt.ses.srv.env.ocienv),           //CONST dvoid   *parenth,
		(*unsafe.Pointer)(unsafe.Pointer(&ociLobLocator)), //dvoid         **descpp,
		C.OCI_DTYPE_LOB,                                   //ub4           type,
		0,                                                 //size_t        xtramem_sz,
		nil)                                               //dvoid         **usrmempp);
	if r == C.OCI_ERROR {
		return nil, nil, stmt.ses.srv.env.ociError()
	} else if r == C.OCI_INVALID_HANDLE {
		return nil, nil, errNew("unable to allocate oci lob handle during bind")
	}

	// Create temporary lob
	r = C.OCILobCreateTemporary(
		stmt.ses.ocisvcctx,      //OCISvcCtx          *svchp,
		stmt.ses.srv.env.ocierr, //OCIError           *errhp,
		ociLobLocator,           //OCILobLocator      *locp,
		C.OCI_DEFAULT,           //ub2                csid,
		C.SQLCS_IMPLICIT,        //ub1                csfrm,
		C.OCI_TEMP_BLOB,         //ub1                lobtype,
		C.TRUE,                  //boolean            cache,
		C.OCI_DURATION_SESSION)  //OCIDuration        duration);
	if r == C.OCI_ERROR {
		// free lob locator handle
		C.OCIDescriptorFree(
			unsafe.Pointer(ociLobLocator), //void     *descp,
			C.OCI_DTYPE_LOB)               //ub4      type );
		return nil, nil, stmt.ses.srv.env.ociError()
	}

	return ociLobLocator, func() {
		C.OCILobFreeTemporary(
			stmt.ses.ocisvcctx,      //OCISvcCtx          *svchp,
			stmt.ses.srv.env.ocierr, //OCIError           *errhp,
			ociLobLocator)           //OCILobLocator      *locp,
		// free lob locator handle
		C.OCIDescriptorFree(
			unsafe.Pointer(ociLobLocator), //void     *descp,
			C.OCI_DTYPE_LOB)               //ub4      type );
	}, nil
}
