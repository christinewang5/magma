/**
 * Copyright 2020 The Magma Authors.
 *
 * This source code is licensed under the BSD-style license found in the
 * LICENSE file in the root directory of this source tree.
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * @flow
 * @format
 */

import type {
  apn_list,
  core_network_types,
  subscriber,
} from '../../../generated/MagmaAPIBindings';

import Button from '../../../fbc_js_core/ui/components/design-system/Button';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogTitle from '@material-ui/core/DialogTitle';
import FormControl from '@material-ui/core/FormControl';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import React from 'react';
import Select from '@material-ui/core/Select';
import TextField from '@material-ui/core/TextField';
import TypedSelect from '../../../fbc_js_core/ui/components/TypedSelect';

import MagmaV1API from '../../../generated/WebClient';
import nullthrows from '../../../fbc_js_core/util/nullthrows';
import {
  base64ToHex,
  hexToBase64,
  isValidHex,
} from '../../../fbc_js_core/util/strings';
import {makeStyles} from '@material-ui/styles';
import {useEnqueueSnackbar} from '../../../fbc_js_core/ui/hooks/useSnackbar';
import {useParams} from 'react-router-dom';
import {useState} from 'react';

const useStyles = makeStyles(() => ({
  input: {
    display: 'inline-flex',
    margin: '5px 0',
    width: '100%',
  },
}));

type EditingSubscriber = {
  imsiID: string,
  lteState: 'ACTIVE' | 'INACTIVE',
  authKey: string,
  authOpc: string,
  forbiddenNetworkTypes: core_network_types,
  subProfile: string,
  apnList: apn_list,
};

type Props = {
  onClose: () => void,
  onSave: (subscriberID: string) => void,
  onSaveError: (reason: string) => void,
  editingSubscriber?: subscriber,
  subProfiles: Array<string>,
  forbiddenNetworkTypes: core_network_types,
  apns: apn_list,
};

function buildEditingSubscriber(
  editingSubscriber: ?subscriber,
): EditingSubscriber {
  if (!editingSubscriber) {
    return {
      imsiID: '',
      lteState: 'ACTIVE',
      authKey: '',
      authOpc: '',
      forbiddenNetworkTypes: [],
      subProfile: 'default',
      apnList: [],
    };
  }

  const authKey = editingSubscriber.lte.auth_key
    ? base64ToHex(editingSubscriber.lte.auth_key)
    : '';

  const authOpc =
    editingSubscriber.lte.auth_opc != undefined
      ? base64ToHex(editingSubscriber.lte.auth_opc)
      : '';

  return {
    imsiID: editingSubscriber.id,
    lteState: editingSubscriber.lte.state,
    authKey,
    authOpc,
    forbiddenNetworkTypes: editingSubscriber.forbidden_network_types || [],
    subProfile: editingSubscriber.lte.sub_profile,
    apnList: editingSubscriber.active_apns || [],
  };
}

export default function AddEditSubscriberDialog(props: Props) {
  const classes = useStyles();
  const params = useParams();
  const enqueueSnackbar = useEnqueueSnackbar();
  const [editingSubscriber, setEditingSubscriber] = useState(
    buildEditingSubscriber(props.editingSubscriber),
  );

  const onSave = () => {
    if (!editingSubscriber.imsiID || !editingSubscriber.authKey) {
      enqueueSnackbar('Please complete all fields', {variant: 'error'});
      return;
    }

    let {imsiID} = editingSubscriber;
    if (!imsiID.startsWith('IMSI')) {
      imsiID = `IMSI${imsiID}`;
    }

    const data = {
      id: imsiID,
      lte: {
        state: editingSubscriber.lteState,
        auth_algo: 'MILENAGE', // default auth algo
        auth_key: editingSubscriber.authKey,
        auth_opc: editingSubscriber.authOpc || undefined,
        sub_profile: editingSubscriber.subProfile,
      },
      forbidden_network_types: editingSubscriber.forbiddenNetworkTypes,
      active_apns: editingSubscriber.apnList,
    };
    if (data.lte.auth_key && isValidHex(data.lte.auth_key)) {
      data.lte.auth_key = hexToBase64(data.lte.auth_key);
    }
    if (data.lte.auth_opc != undefined && isValidHex(data.lte.auth_opc)) {
      data.lte.auth_opc = hexToBase64(data.lte.auth_opc);
    }
    if (props.editingSubscriber) {
      MagmaV1API.putLteByNetworkIdSubscribersBySubscriberId({
        networkId: nullthrows(params.networkId),
        subscriberId: data.id,
        subscriber: data,
      })
        .then(() => props.onSave(data.id))
        .catch(e => props.onSaveError(e.response.data.message));
    } else {
      MagmaV1API.postLteByNetworkIdSubscribers({
        networkId: params.networkId || '',
        subscribers: [data],
      })
        .then(() => props.onSave(data.id))
        .catch(e => props.onSaveError(e.response.data.message));
    }
  };

  return (
    <Dialog open={true} onClose={props.onClose}>
      <DialogTitle>
        {props.editingSubscriber ? 'Edit Subscriber' : 'Add Subscriber'}
      </DialogTitle>
      <DialogContent>
        <TextField
          label="IMSI"
          className={classes.input}
          disabled={!!props.editingSubscriber}
          value={editingSubscriber.imsiID}
          onChange={({target}) =>
            setEditingSubscriber({
              ...editingSubscriber,
              imsiID: target.value,
            })
          }
        />
        <FormControl className={classes.input}>
          <InputLabel htmlFor="lteState">LTE Subscription State</InputLabel>
          <TypedSelect
            inputProps={{id: 'lteState'}}
            value={editingSubscriber.lteState}
            items={{
              ACTIVE: 'Active',
              INACTIVE: 'Inactive',
            }}
            onChange={lteState =>
              setEditingSubscriber({...editingSubscriber, lteState})
            }
          />
        </FormControl>
        <TextField
          label="LTE Auth Key"
          className={classes.input}
          value={editingSubscriber.authKey}
          onChange={({target}) =>
            setEditingSubscriber({
              ...editingSubscriber,
              authKey: target.value,
            })
          }
        />
        <TextField
          label="LTE Auth OPc"
          className={classes.input}
          value={editingSubscriber.authOpc}
          onChange={({target}) =>
            setEditingSubscriber({
              ...editingSubscriber,
              authOpc: target.value,
            })
          }
        />
        <FormControl className={classes.input}>
          <InputLabel htmlFor="subProfile">Data Plan</InputLabel>
          <Select
            inputProps={{id: 'subProfile'}}
            value={editingSubscriber.subProfile}
            onChange={({target}) =>
              setEditingSubscriber({
                ...editingSubscriber,
                subProfile: target.value,
              })
            }>
            {props.subProfiles.map(p => (
              <MenuItem value={p} key={p}>
                {p}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl className={classes.input}>
          <InputLabel htmlFor="forbiddenNetworkTypes">
            Forbidden Network Types
          </InputLabel>
          <Select
            inputProps={{id: 'forbiddenNetworkTypes'}}
            value={editingSubscriber.forbiddenNetworkTypes}
            multiple={false}
            onChange={({target}) =>
              setEditingSubscriber({
                ...editingSubscriber,
                forbiddenNetworkTypes: ((target.value: any): core_network_types),
              })
            }>
            {props.forbiddenNetworkTypes.map(nw => (
              <MenuItem value={nw} key={nw}>
                {nw}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        <FormControl className={classes.input}>
          <InputLabel htmlFor="apnList">Access Point Names</InputLabel>
          <Select
            inputProps={{id: 'apnList'}}
            value={editingSubscriber.apnList}
            multiple={true}
            onChange={({target}) =>
              setEditingSubscriber({
                ...editingSubscriber,
                apnList: ((target.value: any): apn_list),
              })
            }>
            {props.apns.map(apn => (
              <MenuItem value={apn} key={apn}>
                {apn}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </DialogContent>
      <DialogActions>
        <Button onClick={props.onClose} skin="regular">
          Cancel
        </Button>
        <Button onClick={onSave}>Save</Button>
      </DialogActions>
    </Dialog>
  );
}
