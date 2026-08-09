#!/bin/sh
# Run libiscsi test cases

# Assuming libiscsi binaries are built and the gotgt daemon is running

LOCALHOSTPORT=127.0.0.1:3260
TARGET=iqn.2016-09.com.gotgt.gostor:example_tgt_0
TESTCU=${LIBISCSIPATH}/test-tool/iscsi-test-cu

${LIBISCSIPATH}/utils/iscsi-ls -s iscsi://${LOCALHOSTPORT}/${TARGET}
echo 
${LIBISCSIPATH}/utils/iscsi-inq iscsi://${LOCALHOSTPORT}/${TARGET}/0
echo
${LIBISCSIPATH}/utils/iscsi-readcapacity16 iscsi://${LOCALHOSTPORT}/${TARGET}/0

NEWCASES="ALL.PrinReadKeys"

TESTCASES="ALL.Inquiry.Standard\
           ALL.Inquiry.AllocLength ALL.Inquiry.MandatoryVPDSBC\
           ALL.Inquiry.SupportedVPD ALL.Inquiry.VersionDescriptors \
           ALL.Inquiry.EVPD ALL.Mandatory ALL.ModeSense6 ALL.NoMedia \
           ALL.Prefetch10 ALL.Prefetch16 ALL.PreventAllow  \
           ALL.ReadCapacity10 ALL.ReadCapacity16 ALL.Read6  \
           ALL.Read10 ALL.Read12 ALL.Read16 ALL.ReadOnly \
           ALL.ReportSupportedOpcodes.Simple ALL.Reserve6.Simple \
           ALL.StartStopUnit ALL.TestUnitReady \
           ALL.Write10 ALL.Write16 ALL.Write12 ALL.WriteVerify10 \
           ALL.WriteVerify16 ALL.WriteVerify12 \
	   ALL.WriteSame10.Simple ALL.WriteSame16.Simple \
           ALL.Verify10 ALL.Verify12 ALL.Verify16 \
           ALL.iSCSITMF ALL.iSCSIcmdsn \
           ALL.Unmap.Simple ALL.Unmap.VPD ALL.Unmap.ZeroBlocks \
	   "

#for i in $NEWCASES

echo "\n====== Test started"
date
for i in $TESTCASES
do {
	${TESTCU} -d -A --test=$i iscsi://${LOCALHOSTPORT}/${TARGET}/0
	echo "===";
} done
date
echo "====== Test ended"

# sanity check example below
# ./libiscsi-gotgt-test.sh > libiscsi-runoutput.txt 2>&1 &
# grep "Test:" libiscsi-runoutput.txt | wc
# grep "passed" libiscsi-runoutput.txt | wc

exit 0
