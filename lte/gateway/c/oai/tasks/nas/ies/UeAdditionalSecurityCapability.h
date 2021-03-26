/*
 * Licensed to the OpenAirInterface (OAI) Software Alliance under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The OpenAirInterface Software Alliance licenses this file to You under
 * the terms found in the LICENSE file in the root of this source tree.
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *-------------------------------------------------------------------------------
 * For more information about the OpenAirInterface (OAI) Software Alliance:
 *      contact@openairinterface.org
 */
#pragma once

#include <stdint.h>

#define UE_ADDITIONAL_SECURITY_CAPABILITY_MINIMUM_LENGTH 6
#define UE_ADDITIONAL_SECURITY_CAPABILITY_MAXIMUM_LENGTH 6

typedef struct ue_additional_security_capability_s {
  uint16_t _5g_ea;
  uint16_t _5g_ia;
} ue_additional_security_capability_t;

int encode_ue_additional_security_capability(
    ue_additional_security_capability_t* uasc, uint8_t iei, uint8_t* buffer,
    uint32_t len);

int decode_ue_additional_security_capability(
    ue_additional_security_capability_t* uasc, uint8_t iei, uint8_t* buffer,
    uint32_t len);