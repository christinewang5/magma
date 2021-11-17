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

package servicers

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"magma/orc8r/cloud/go/clock"
	"magma/orc8r/cloud/go/identity"
	certprotos "magma/orc8r/cloud/go/services/certifier/protos"
	"magma/orc8r/cloud/go/services/certifier/storage"
	"magma/orc8r/lib/go/errors"
	"magma/orc8r/lib/go/protos"
	"magma/orc8r/lib/go/security/cert"
	unarylib "magma/orc8r/lib/go/service/middleware/unary"
)

var (
	NumTrialsForSn      int
	CollectGarbageAfter time.Duration // remove cert if expired for certain amount of time
)

func init() {
	NumTrialsForSn = 1
	CollectGarbageAfter = time.Hour * 24
}

type CAInfo struct {
	Cert    *x509.Certificate
	PrivKey interface{}
}

type CertifierServer struct {
	store storage.CertifierStorage
	CAs   map[protos.CertType]*CAInfo
}

func NewCertifierServer(store storage.CertifierStorage, CAs map[protos.CertType]*CAInfo) (srv *CertifierServer, err error) {
	srv = new(CertifierServer)
	srv.store = store
	if CAs == nil {
		return nil, fmt.Errorf("CA info not provided to certifier")
	}
	if len(CAs) == 0 {
		return nil, fmt.Errorf("No Certificates are provided to certifier")
	}
	srv.CAs = CAs
	return srv, nil
}

func generateSerialNumber(store storage.CertifierStorage) (sn *big.Int, err error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)

	for i := 0; i < NumTrialsForSn; i++ {
		sn, err = rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, fmt.Errorf("Failed to generate serial number: %s", err)
		}
		_, err := store.GetCertInfo(cert.SerialToString(sn))
		if err != nil {
			return sn, nil
		}
	}
	return nil, fmt.Errorf(
		"Failed to genearte serial number after %d trials.", NumTrialsForSn)
}

func parseAndCheckCSR(csrDER []byte) (*x509.CertificateRequest, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse certificate request: %s", err)
	}

	err = csr.CheckSignature()
	if err != nil {
		return nil, fmt.Errorf("Failed to check certificate request signature: %s", err)
	}
	return csr, err
}

func (srv *CertifierServer) signCSR(
	csr *x509.CertificateRequest,
	sn *big.Int,
	certType protos.CertType,
	validTime time.Duration,
) ([]byte, time.Time, time.Time, error) {

	if srv.CAs == nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("CAInfo not found")
	}
	ca, ok := srv.CAs[certType]
	if !ok {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("No CA found for given cert type: %s", certType.String())
	}
	signingCert := ca.Cert
	signingKey := ca.PrivKey

	now := clock.Now().UTC()
	// Provide a cert from an hour ago to account for clock skews
	notBefore := now.Add(-1 * time.Hour)
	notAfter := now.Add(validTime)
	if notAfter.After(signingCert.NotAfter) {
		glog.Warningln("The requested time is longer than signing certificate valid time.")
		notAfter = signingCert.NotAfter
	}
	template := x509.Certificate{
		SerialNumber:          sn,
		Subject:               csr.Subject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	clientCertDER, err := x509.CreateCertificate(
		rand.Reader, &template, signingCert, csr.PublicKey, signingKey)
	if err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("Failed to sign csr: %s", err)
	}

	return clientCertDER, notBefore, notAfter, nil
}

func checkOrOverwriteCN(csr *x509.CertificateRequest, csrMsg *protos.CSR) error {
	id := csrMsg.Id
	idCn := id.ToCommonName()
	if idCn == nil {
		return nil
	}
	if len(csr.Subject.CommonName) == 0 {
		csr.Subject.CommonName = *idCn
		return nil
	}

	if csr.Subject.CommonName != *idCn {
		return status.Errorf(
			codes.Aborted,
			"CN from CSR (%s) and CN in Identity (%s) do not match", csr.Subject.CommonName, *idCn)
	}

	if csrMsg.CertType == protos.CertType_VPN && identity.IsGateway(id) {
		// Use networkID & logicalID to identify the vpn client instead of hwID
		gw := id.GetGateway()
		csr.Subject.CommonName = gw.GetLogicalId()
	}

	return nil
}

func (srv *CertifierServer) getCertInfo(sn string) (*certprotos.CertificateInfo, error) {
	certInfo, err := srv.store.GetCertInfo(sn)
	if err != nil {
		return &certprotos.CertificateInfo{}, status.Errorf(codes.NotFound, "Failed to load certificate: %s", err)
	}
	return certInfo, nil
}

// Verify that the certificate is signed by our CA
func (srv *CertifierServer) verifyCert(clientCert *x509.Certificate, certType protos.CertType) error {
	// Check if CAInfo / cert exists for requested cert type
	if srv.CAs == nil {
		return fmt.Errorf("CAInfo not found")
	}
	ca, ok := srv.CAs[certType]
	if !ok {
		return fmt.Errorf("No CA found for given cert type: %s", certType.String())
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Cert) // Use appropriate cert to check against
	opts := x509.VerifyOptions{
		Roots:         caPool,
		Intermediates: x509.NewCertPool(),
		// Make sure client cert has ExtKeyUsageClientAuth
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := clientCert.Verify(opts); err != nil {
		return fmt.Errorf("Certificate Verification Failure: %s", err)
	}
	return nil
}

func (srv *CertifierServer) GetCA(ctx context.Context, getCAReqMsg *certprotos.GetCARequest) (*protos.CACert, error) {
	if getCAReqMsg == nil {
		return nil, status.Errorf(codes.InvalidArgument, "Invalid CA request")
	}

	ca, ok := srv.CAs[getCAReqMsg.CertType]
	if !ok {
		return nil, fmt.Errorf("no CA found for given CA type: %s", getCAReqMsg.CertType.String())
	}

	caCertMsg := &protos.CACert{Cert: ca.Cert.Raw}

	return caCertMsg, nil
}

func (srv *CertifierServer) SignAddCertificate(ctx context.Context, csrMsg *protos.CSR) (*protos.Certificate, error) {

	sn, err := generateSerialNumber(srv.store)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Error generating serial number: %s", err)
	}

	csr, err := parseAndCheckCSR(csrMsg.CsrDer)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Error parsing CSR: %s", err)
	}

	err = checkOrOverwriteCN(csr, csrMsg)
	if err != nil {
		return nil, err
	}

	validTime, err := ptypes.Duration(csrMsg.ValidTime)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Invalid requested certificate duration: %s", err)
	}

	certDER, notBefore, notAfter, err := srv.signCSR(csr, sn, csrMsg.CertType, validTime)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Error signing CSR: %s", err)
	}

	notBeforeProto, _ := ptypes.TimestampProto(notBefore)
	notAfterProto, _ := ptypes.TimestampProto(notAfter)

	// create CertificateInfo
	certInfo := &certprotos.CertificateInfo{
		Id:        csrMsg.Id,
		CertType:  csrMsg.CertType,
		NotBefore: notBeforeProto,
		NotAfter:  notAfterProto,
	}
	// add to table
	snString := cert.SerialToString(sn)
	// Ensure serial number is not the orc8r client reserved SN
	if snString == unarylib.ORC8R_CLIENT_CERT_VALUE {
		return nil, status.Errorf(codes.InvalidArgument, "Invalid Serial Number")
	}
	err = srv.store.PutCertInfo(snString, certInfo)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Error adding CertificateInfo: %s", err)
	}

	// create Certificate
	certMsg := protos.Certificate{
		Sn:        &protos.Certificate_SN{Sn: snString},
		NotBefore: notBeforeProto,
		NotAfter:  notAfterProto,
		CertDer:   certDER,
	}
	return &certMsg, nil
}

func (srv *CertifierServer) GetIdentity(
	ctx context.Context, snMsg *protos.Certificate_SN) (*certprotos.CertificateInfo, error) {

	var certSN string
	if snMsg != nil {
		certSN = strings.TrimLeft(snMsg.Sn, "0")
	}
	certInfo, err := srv.store.GetCertInfo(certSN)
	if err != nil {
		return &certprotos.CertificateInfo{}, status.Errorf(
			codes.NotFound, "Certificate with serial number '%s' is not found", certSN)
	}

	// check timestamp
	notBefore, _ := ptypes.Timestamp(certInfo.NotBefore)
	notAfter, _ := ptypes.Timestamp(certInfo.NotAfter)
	now := clock.Now().UTC()
	if now.After(notAfter) {
		return &certprotos.CertificateInfo{}, status.Errorf(codes.OutOfRange,
			"Certificate with serial number '%s' has expired", certSN)
	}
	if now.Before(notBefore) {
		return &certprotos.CertificateInfo{}, status.Errorf(codes.OutOfRange,
			"Certificate with serial number '%s' is not yet valid", certSN)
	}
	return certInfo, nil
}

func (srv *CertifierServer) RevokeCertificate(
	ctx context.Context, snMsg *protos.Certificate_SN) (*protos.Void, error) {

	var certSN string
	if snMsg != nil {
		certSN = strings.TrimLeft(snMsg.Sn, "0")
	}
	_, err := srv.store.GetCertInfo(certSN)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Cannot find certificate with SN: %s", certSN)
	}
	err = srv.store.DeleteCertInfo(certSN)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "Failed to delete certificate: %s", err)
	}
	return &protos.Void{}, nil
}

func (srv *CertifierServer) AddCertificate(ctx context.Context, req *certprotos.AddCertRequest) (*protos.Void, error) {

	res := &protos.Void{}
	x509Cert, err := x509.ParseCertificate(req.CertDer)
	if err != nil {
		return res,
			status.Errorf(codes.InvalidArgument, "DER Parse Error: %s", err)
	}
	if x509Cert.SerialNumber == nil {
		return res, status.Errorf(codes.InvalidArgument, "Invalid Serial Number")
	}
	snStr := cert.SerialToString(x509Cert.SerialNumber)
	// Ensure serial number is not the orc8r client reserved SN
	if snStr == unarylib.ORC8R_CLIENT_CERT_VALUE {
		return res, status.Errorf(codes.InvalidArgument, "Invalid Serial Number")
	}
	// Verify that the certificate is signed by our CA
	if err = srv.verifyCert(x509Cert, req.CertType); err != nil {
		return res, status.Errorf(
			codes.InvalidArgument, "%s for Certificate SN %s", err, snStr)
	}
	// Check if a certificate with the same SN is already there
	_, err = srv.store.GetCertInfo(snStr)
	if err == nil {
		return res, status.Errorf(
			codes.AlreadyExists, "Certificate SN %s already exists", snStr)
	}
	// create CertificateInfo
	notBeforeProto, _ := ptypes.TimestampProto(x509Cert.NotBefore)
	notAfterProto, _ := ptypes.TimestampProto(x509Cert.NotAfter)
	certInfo := &certprotos.CertificateInfo{
		Id:        req.Id,
		CertType:  req.CertType,
		NotBefore: notBeforeProto,
		NotAfter:  notAfterProto,
	}
	// add to table
	err = srv.store.PutCertInfo(snStr, certInfo)
	if err != nil {
		return res,
			status.Errorf(codes.Internal, "Error adding CertificateInfo: %s", err)
	}
	return res, nil
}

// Finds & returns Serial Numbers of all Certificates associated with the
// given Identity
func (srv *CertifierServer) FindCertificates(ctx context.Context, id *protos.Identity) (*certprotos.SerialNumbers, error) {

	res := &certprotos.SerialNumbers{}
	if id != nil {
		idKey := id.HashString()
		snList, err := srv.ListCertificates(ctx, &protos.Void{})
		if err != nil {
			return res, err
		}
		for _, sn := range snList.Sns {
			certInfo, err := srv.getCertInfo(sn)
			if err != nil {
				return res, err
			}
			if certInfo != nil && certInfo.Id.HashString() == idKey {
				res.Sns = append(res.Sns, sn)
			}
		}
	}
	return res, nil
}

// Returns serial numbers of all certificates in the table
func (srv *CertifierServer) ListCertificates(ctx context.Context, void *protos.Void) (*certprotos.SerialNumbers, error) {
	res := &certprotos.SerialNumbers{}
	snList, err := srv.store.ListSerialNumbers()
	if err != nil {
		return res, status.Errorf(
			codes.Internal, "Failed to get certificate serial numbers: %s", err)
	}
	res.Sns = snList
	return res, nil
}

// GetAll returns all Certificates Records
func (srv *CertifierServer) GetAll(context.Context, *protos.Void) (*certprotos.CertificateInfoMap, error) {
	res := &certprotos.CertificateInfoMap{Certificates: map[string]*certprotos.CertificateInfo{}}
	certInfos, err := srv.store.GetAllCertInfo()
	if err != nil {
		return res, status.Errorf(codes.Internal, "Failed to get all certificates: %v", err)
	}
	res.Certificates = certInfos
	return res, nil
}

func (srv *CertifierServer) CollectGarbage(ctx context.Context, void *protos.Void) (*protos.Void, error) {
	count, err := srv.CollectGarbageImpl(ctx)
	glog.Infof("purged %d expired certificates", count)
	return &protos.Void{}, err
}

func (srv *CertifierServer) CollectGarbageImpl(ctx context.Context) (int, error) {
	snList, err := srv.ListCertificates(ctx, &protos.Void{})
	if err != nil {
		return 0, err
	}
	var multiErr *errors.Multi
	count := 0
	for _, sn := range snList.Sns {
		certInfo, err := srv.getCertInfo(sn)
		if err != nil {
			multiErr.AddFmt(err, "'%s' get info error:", sn)
		}
		notAfter, _ := ptypes.Timestamp(certInfo.NotAfter)
		notAfter = notAfter.Add(CollectGarbageAfter)
		if time.Now().UTC().After(notAfter) {
			err = srv.store.DeleteCertInfo(sn)
			if err != nil {
				multiErr.AddFmt(err, "'%s' delete error:", sn)
			} else {
				count += 1
			}
		}
	}
	if multiErr.AsError() != nil {
		glog.Errorf("Failed to delete certificate[s]: %v", multiErr)
		return count, status.Error(codes.Internal, multiErr.Error())
	}
	return count, nil
}

// GetOperatorTokens gets all operator tokens after authentication
func (srv *CertifierServer) GetOperatorTokens(ctx context.Context, getOpReq *certprotos.GetOperatorRequest) (*certprotos.Operator_TokenList, error) {
	username := getOpReq.GetUsername()
	user, err := srv.store.GetHTTPBasicAuth(username)
	if err != nil {
		return &certprotos.Operator_TokenList{}, status.Errorf(
			codes.Internal, "failed to fetch user %s from database: %v", username, err)
	}
	// check if token is registered with user
	// TODO(christinewang5): ugh why is finding things in list so hard? should i use a map?
	token := getOpReq.GetToken()
	flag := false
	tokens := user.GetTokens()
	for _, t := range tokens.GetToken() {
		if t == token {
			flag = true
		}
	}
	if !flag {
		return &certprotos.Operator_TokenList{}, status.Errorf(codes.PermissionDenied, "token %s is not registered with user %s", token, user)
	}
	return tokens, nil
}

func (srv *CertifierServer) GetPolicyDecision(ctx context.Context, getPDReq *certprotos.GetPolicyDecisionRequest) (*certprotos.PolicyDecision, error) {
	tokens := getPDReq.TokenList
	resource := getPDReq.Resource
	action := getPDReq.RequestAction
	for _, t := range tokens.GetToken() {
		effect, _ := srv.getPolicyDecisionFromToken(t, resource, action)
		// return the policy decision once we encounter the first allow or deny
		// TODO(christinewang5) hmm which one would take precedent if multiple polices for the same resource?
		switch effect {
		case certprotos.Effect_ALLOW:
		case certprotos.Effect_DENY:
			return &certprotos.PolicyDecision{Effect: effect}, nil
		default:
			continue
		}
	}
	return nil, nil
}

// check if the request
func (srv *CertifierServer) getPolicyDecisionFromToken(token string, resource string, action certprotos.Action) (certprotos.Effect, error) {
	policy, err := srv.store.GetPolicy(token)
	if err != nil {
		status.Errorf(codes.Internal, "failed to get policy from db: %v", err)
	}
	effect, err := checkResource(resource, policy)
	// return if the policy denies the user or is unknown for the resource
	if effect == certprotos.Effect_DENY || effect == certprotos.Effect_UNKNOWN {
		return effect, status.Errorf(codes.PermissionDenied, "not authorized to read/write resource")
	} else if err != nil {
		return effect, err
	}
	// checks if user can read/write the resource
	effect = checkAction(action, policy)
	return effect, nil
}

// checkResource checks if the requested resource is authorized by the policy
func checkResource(resource string, policy *certprotos.Policy) (certprotos.Effect, error) {
	effect := policy.GetEffect()
	// checks if any of policy's resource list allows/denies the requested resource
	for _, pr := range policy.GetResources().GetResource() {
		// TODO(christinewang5): kind of abusing the notion of filepaths here but oh well
		if ok, err := filepath.Match(resource, pr); ok {
			return effect, err
		} else {
			glog.Errorf("failed to match resource path %v", err)
		}
	}
	// defaults to unknown if resource is not explicitly allowed/denied by policy
	return effect, nil
}

// checkAction checks if the requested action is authorized by the policy
func checkAction(action certprotos.Action, policy *certprotos.Policy) certprotos.Effect {
	if policy.GetAction() == certprotos.Action_WRITE {
		return certprotos.Effect_ALLOW
	} else if policy.GetAction() == certprotos.Action_READ && action == certprotos.Action_READ {
		return certprotos.Effect_ALLOW
	}
	return certprotos.Effect_DENY
}
