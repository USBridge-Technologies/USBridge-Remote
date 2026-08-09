## Test Coverage
Note that open-iscsi and libiscsi are separate iSCSI initiator packages 
and have no interdependency.  We should test the target library with both 
packages to maximize coverage.
The open-iscsi has a kernel component and it will rebuild kernel after 
inserting a few modules. If you have many kernel bootable images, 
your should remember which one to use for open-iscsi testing.

### Test Scripts
Five separate example shell scripts are here to facilitate and automate 
testing, although automatic testing will be submitted to travis.ci.com via 
the .travis.yml file. As more features are developed and bugs are fixed, 
please remember to update both the test scripts and the .yml file.

Manual testing by the developers and testers should consult the 
setup-dev-test.sh, gotgt-daemon.sh libiscsi-gotgt-test.sh,
openiscsi-gotgt-test.sh and test-cleanup.sh files.
You need to set up two exported environment variable LIBISCSIPATH and GOTGTPATH.
These are the location where to have the source code for libiscsi and gotgt.
Normally, they should be set once for all in your home directory's .bashrc file
for conveniences. Development and test environment setup is normally done once 
per client and updated only as required.  Kicking off target daemon is needed 
before testing and after a successful rebuild of the target library.

The shell script names implies the intended functions,
You should run setup-dev-test.sh first and then gotgt-daemon.sh.
Some test scripts leave state information in your environment and make the 
repeat testing unpredictable.  You can consult and run test-cleanup.sh to do
some state cleaning.
These have been tested with Ubuntu Linux versions as of 2017.
Please update them as more Linux variants and platforms are verified or added.

### Noises in the test results
Note that the "[FAILED]" lines during the libiscsi test are often 
due to the standard procedure to check the unknown devices with inquiry command.
It will not impact the real test.  The "CUnit" testing noises can also 
be safely ignored.
Also note that some testing using fdisk and mkfs in the openiscsi-gotgt-test.sh
create state information for the partition and the file system.
You should run the test-cleanup.sh to remove those state information.
That is not foolproof however, and you may need to do some manually.

## Making contributions
As a way to get you started and get you familiar with the gotgt project,
you can check out this file at issue [#55]
(https://github.com/gostor/gotgt/issues/55)
to improve the target library code to pass more test cases (by libiscsi). 
Some are relatively straightforward.
