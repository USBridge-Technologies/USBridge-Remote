#!/bin/sh
# Run open-iscsi test cases

# Assuming open-iscsi package and binaries are installed properly, and 
# the gotgt daemon is running

LOCALHOST=127.0.0.1

# track test environment
date
uname -a
df -hT
sudo lsblk -l

echo "==== iscsi initiator test"

# some simple iscsi initiator tests
sudo iscsiadm -m discovery -t sendtargets -p ${LOCALHOST}
echo
sudo iscsiadm -m node -L all
echo
sudo iscsiadm -m session
echo "==== end of iscsi initiator test"

# Assuming /dev/sdb is the disk presented at the iSCSI backend
# You don't want to mess up with your true /dev/sdb in the system if there is
#  one already. You need to modify this script and other test scripts as needed.
# Let's confirm that, and error out if necessary. 

# Might need to add clean-up scripts to unmount over /var/tmp/test
# sudo umount /var/tmp/test
mount | grep sdb1
mount | grep "var/tmp/test"
if [ $? -eq 0 ]
then
	sudo umount /var/tmp/test
fi

sudo fdisk -l
echo "====Examine disk /dev/sdb to be be sure ..."
sudo fdisk -l | grep "Disk /dev/sdb: 100 MiB" 
if [ $? -ne 0 ]
then
	echo "Warning: /dev/sdb: 100 MiB not found!"
	echo "Revise your test script as required."
	exit 1
fi
echo "Continue...."

echo "=== Create a partition, mkfs, mount and do some I/O"

## Mount and prepare a test directory for open-iscsi testing
##
## n: add a new partition
## p: primary partition
## 1: partition number
## \n: use default (2048) for the first sector
## \n: use default (20479) for the last sector
###   This will create a new partition 1 of type 'Linux' and of size 9 MiB.
## t: change partition type
## c: change to W95 FAT32 (LBA)
## a: Enable the bootable flag for partition 1
## 1: (unknown command ????? XXX)
## w: write the table to disk and exit

# write a partition table

# In order for the following to work,
# Delete existing /dev/sdb1 partition if found
sudo lsblk -l | grep sdb1 
if [ $? -eq 0 ]
then
	/bin/echo -e "p\nd\nw" | sudo fdisk /dev/sdb
fi

/bin/echo -e "n\np\n1\n\n\nt\nc\na\n1\nw" | sudo fdisk /dev/sdb

# it might prompt for confirmation if previous file system is detected
sudo mkfs.ext3 /dev/sdb1

sudo mkdir -p /var/tmp/test
sudo mount /dev/sdb1 /var/tmp/test

mount | grep sdb1
sudo ls -lh /var/tmp/test/

##
## TO-DO we can do more open-iscsi testing just with this.
##
#
sudo chmod 777 /var/tmp/test
# should measure performance with large count below on a huge file system
time sudo dd if=/dev/mem of=/var/tmp/test/mem-file bs=4096 count=100
cp /var/tmp/test/mem-file /var/tmp/test/mem-file-2
md5sum /var/tmp/test/mem-file
md5sum /var/tmp/test/mem-file-2

### umount and remount the file system
sudo umount /var/tmp/test
sudo mount /dev/sdb1 /var/tmp/test
md5sum /var/tmp/test/mem-file
md5sum /var/tmp/test/mem-file-2

# umount and some clean-up 
sudo umount /var/tmp/test

exit 0
