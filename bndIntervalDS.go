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
	"unsafe"
)

type bndIntervalDS struct {
	stmt        *Stmt
	ocibnd      *C.OCIBind
	ociInterval *C.OCIInterval
}

func (bnd *bndIntervalDS) bind(value IntervalDS, position int, stmt *Stmt) error {
	bnd.stmt = stmt
	r := C.OCIDescriptorAlloc(
		unsafe.Pointer(bnd.stmt.ses.srv.env.ocienv),         //CONST dvoid   *parenth,
		(*unsafe.Pointer)(unsafe.Pointer(&bnd.ociInterval)), //dvoid         **descpp,
		C.OCI_DTYPE_INTERVAL_DS,                             //ub4           type,
		0,   //size_t        xtramem_sz,
		nil) //dvoid         **usrmempp);
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	} else if r == C.OCI_INVALID_HANDLE {
		return errNew("unable to allocate oci interval handle during bind")
	}
	r = C.OCIIntervalSetDaySecond(
		unsafe.Pointer(bnd.stmt.ses.srv.env.ocienv), //void               *hndl,
		bnd.stmt.ses.srv.env.ocierr,                 //OCIError           *err,
		C.sb4(value.Day),                            //sb4                dy,
		C.sb4(value.Hour),                           //sb4                hr,
		C.sb4(value.Minute),                         //sb4                mm,
		C.sb4(value.Second),                         //sb4                ss,
		C.sb4(value.Nanosecond),                     //sb4                fsec,
		bnd.ociInterval)                             //OCIInterval        *result );
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	}
	r = C.OCIBINDBYPOS(
		bnd.stmt.ocistmt,                              //OCIStmt      *stmtp,
		(**C.OCIBind)(&bnd.ocibnd),                    //OCIBind      **bindpp,
		bnd.stmt.ses.srv.env.ocierr,                   //OCIError     *errhp,
		C.ub4(position),                               //ub4          position,
		unsafe.Pointer(&bnd.ociInterval),              //void         *valuep,
		C.LENGTH_TYPE(unsafe.Sizeof(bnd.ociInterval)), //sb8          value_sz,
		C.SQLT_INTERVAL_DS,                            //ub2          dty,
		nil,                                           //void         *indp,
		nil,                                           //ub2          *alenp,
		nil,                                           //ub2          *rcodep,
		0,                                             //ub4          maxarr_len,
		nil,                                           //ub4          *curelep,
		C.OCI_DEFAULT)                                 //ub4          mode );
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	}
	return nil
}

func (bnd *bndIntervalDS) setPtr() error {
	return nil
}

func (bnd *bndIntervalDS) close() (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = errR(value)
		}
	}()

	C.OCIDescriptorFree(
		unsafe.Pointer(bnd.ociInterval), //void     *descp,
		C.OCI_DTYPE_INTERVAL_DS)         //timeDefine.descTypeCode)
	stmt := bnd.stmt
	bnd.stmt = nil
	bnd.ocibnd = nil
	bnd.ociInterval = nil
	stmt.putBnd(bndIdxIntervalDS, bnd)
	return nil
}
