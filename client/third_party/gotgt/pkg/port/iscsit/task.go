/*
Copyright 2017 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// iSCSI task management
package iscsit

const (
	ISCSI_FLAG_TM_FUNC_MASK byte = 0x7F

	// Function values
	// aborts the task identified by the Referenced Task Tag field
	ISCSI_TM_FUNC_ABORT_TASK = 1
	// aborts all Tasks issued via this session on the logical unit
	ISCSI_TM_FUNC_ABORT_TASK_SET = 2
	// clears the Auto Contingent Allegiance condition
	ISCSI_TM_FUNC_CLEAR_ACA = 3
	// aborts all Tasks in the appropriate task set as defined by the TST field in the Control mode page
	ISCSI_TM_FUNC_CLEAR_TASK_SET     = 4
	ISCSI_TM_FUNC_LOGICAL_UNIT_RESET = 5
	ISCSI_TM_FUNC_TARGET_WARM_RESET  = 6
	ISCSI_TM_FUNC_TARGET_COLD_RESET  = 7
	// reassigns connection allegiance for the task identified by the Referenced Task Tag field to this connection, thus resuming the iSCSI exchanges for the task
	ISCSI_TM_FUNC_TASK_REASSIGN = 8

	// Response values
	// Function complete
	ISCSI_TMF_RSP_COMPLETE = 0x00
	// Task does not exist
	ISCSI_TMF_RSP_NO_TASK = 0x01
	// LUN does not exist
	ISCSI_TMF_RSP_NO_LUN = 0x02
	// Task still allegiant
	ISCSI_TMF_RSP_TASK_ALLEGIANT = 0x03
	// Task allegiance reassignment not supported
	ISCSI_TMF_RSP_NO_FAILOVER = 0x04
	// Task management function not supported
	ISCSI_TMF_RSP_NOT_SUPPORTED = 0x05
	// Function authorization failed
	ISCSI_TMF_RSP_AUTH_FAILED = 0x06
	// Function rejected
	ISCSI_TMF_RSP_REJECTED = 0xff
)
