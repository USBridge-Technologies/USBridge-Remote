#!/bin/sh
# Trying our best to clean up the test environment to repeat the test.
[ ! -n "$LIBISCSIPATH" ] &&  echo "Need LIBISCSIPATH defintion" && exit 1 
[ ! -d $LIBISCSIPATH ] && echo "Need $LIBISCSIPATH directory" && exit 3
[ ! -x /sbin/iscsiadm ] && echo "/sbin/iscsiadm not built" && exit 9

# kill off daemon
#killall gotgt

# empty backend file 
echo > /var/tmp/disk.img

# empty config file
echo > ${HOME}/.gotgt/config.jason

# Delete existing /dev/sdb1 partition if found
sudo lsblk -l | grep sdb1
if [ $? -eq 0 ]
then
	/bin/echo -e "p\nd\nw" | sudo fdisk /dev/sdb
fi

#unmount /var/tmp/test
sudo umount /var/tmp/test

exit 0
