/*
Copyright 2020 The Magma Authors.

This source code is licensed under the BSD-style license found in the
LICENSE file in the root directory of this source tree.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

#pragma once

#include "lte/gateway/c/core/common/common_defs.h"
#include "lte/gateway/c/core/oai/include/sgw_config.h"
#include "lte/gateway/c/core/oai/lib/itti/intertask_interface.h"

extern task_zmq_ctx_t sgw_s8_task_zmq_ctx;

status_code_e sgw_s8_init(sgw_config_t* sgw_config_p);
