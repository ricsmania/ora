// Copyright 2014 Rana Ian. All rights reserved.
// Use of this source code is governed by The MIT License
// found in the accompanying LICENSE file.

package ora

/*
#include <oci.h>
#include <stdlib.h>
#include "version.h"
*/
import "C"
import (
	"bytes"
	"fmt"
	"time"
	"unsafe"
)

type bndTime struct {
	stmt        *Stmt
	ocibnd      *C.OCIBind
	ociDateTime *C.OCIDateTime
	cZone       *C.char
	zoneBuf     bytes.Buffer
}

func (bnd *bndTime) bind(value time.Time, position int, stmt *Stmt) error {
	bnd.stmt = stmt
	zone := zoneOffset(value, &bnd.zoneBuf)
	bnd.cZone = C.CString(zone)
	r := C.OCIDescriptorAlloc(
		unsafe.Pointer(bnd.stmt.ses.srv.env.ocienv),         //CONST dvoid   *parenth,
		(*unsafe.Pointer)(unsafe.Pointer(&bnd.ociDateTime)), //dvoid         **descpp,
		C.OCI_DTYPE_TIMESTAMP_TZ,                            //ub4           type,
		0,   //size_t        xtramem_sz,
		nil) //dvoid         **usrmempp);
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	} else if r == C.OCI_INVALID_HANDLE {
		return errNew("unable to allocate oci timestamp handle during bind")
	}
	r = C.OCIDateTimeConstruct(
		unsafe.Pointer(bnd.stmt.ses.srv.env.ocienv), //dvoid         *hndl,
		bnd.stmt.ses.srv.env.ocierr,                 //OCIError      *err,
		bnd.ociDateTime,                             //OCIDateTime   *datetime,
		C.sb2(value.Year()),                         //sb2           year,
		C.ub1(int32(value.Month())),                 //ub1           month,
		C.ub1(value.Day()),                          //ub1           day,
		C.ub1(value.Hour()),                         //ub1           hour,
		C.ub1(value.Minute()),                       //ub1           min,
		C.ub1(value.Second()),                       //ub1           sec,
		C.ub4(value.Nanosecond()),                   //ub4           fsec,
		(*C.OraText)(unsafe.Pointer(bnd.cZone)),     //OraText       *timezone,
		C.size_t(len(zone)))                         //size_t        timezone_length );
	if r == C.OCI_ERROR {
		return bnd.stmt.ses.srv.env.ociError()
	}
	r = C.OCIBINDBYPOS(
		bnd.stmt.ocistmt,                              //OCIStmt      *stmtp,
		(**C.OCIBind)(&bnd.ocibnd),                    //OCIBind      **bindpp,
		bnd.stmt.ses.srv.env.ocierr,                   //OCIError     *errhp,
		C.ub4(position),                               //ub4          position,
		unsafe.Pointer(&bnd.ociDateTime),              //void         *valuep,
		C.LENGTH_TYPE(unsafe.Sizeof(bnd.ociDateTime)), //sb8          value_sz,
		C.SQLT_TIMESTAMP_TZ,                           //ub2          dty,
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

func (bnd *bndTime) setPtr() (err error) {
	return nil
}

func zoneOffset(value time.Time, buf *bytes.Buffer) string {
	buf.Reset()
	_, zoneOffsetInSeconds := value.Zone()
	if zoneOffsetInSeconds < 0 {
		buf.WriteRune('-')
		zoneOffsetInSeconds *= -1
	} else {
		buf.WriteRune('+')
	}
	hourOffset := zoneOffsetInSeconds / 3600
	buf.WriteString(fmt.Sprintf("%02d", hourOffset))
	buf.WriteRune(':')
	zoneOffsetInSeconds -= hourOffset * 3600
	minuteOffset := zoneOffsetInSeconds / 60
	buf.WriteString(fmt.Sprintf("%02d", minuteOffset))
	return buf.String()
}

func (bnd *bndTime) close() (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = errR(value)
		}
	}()

	if bnd.cZone != nil {
		C.free(unsafe.Pointer(bnd.cZone))
		bnd.cZone = nil
		C.OCIDescriptorFree(
			unsafe.Pointer(bnd.ociDateTime), //void     *descp,
			C.OCI_DTYPE_TIMESTAMP_TZ)        //ub4      type );
	}
	stmt := bnd.stmt
	bnd.stmt = nil
	bnd.ocibnd = nil
	bnd.ociDateTime = nil
	bnd.zoneBuf.Reset()
	stmt.putBnd(bndIdxTime, bnd)
	return nil
}
