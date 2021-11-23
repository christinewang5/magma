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

package tests

import (
	"net/http"
	"testing"

	"github.com/labstack/echo"
	"github.com/stretchr/testify/assert"

	"magma/orc8r/cloud/go/obsidian"
	"magma/orc8r/cloud/go/obsidian/access"
	certifier_test_service "magma/orc8r/cloud/go/services/certifier/test_init"
	"magma/orc8r/cloud/go/services/certifier/test_utils"
	tenantsh "magma/orc8r/cloud/go/services/tenants/obsidian/handlers"
)

func TestMiddlewareWithoutCertifier(t *testing.T) {
	e := startTestMiddlewareServer(t)
	e.Use(access.CertificateMiddleware) // inject obsidian access control middleware
	listener := WaitForTestServer(t, e)

	if listener == nil {
		return // WaitForTestServer should have 'logged' error already
	}

	urlPrefix := "http://" + listener.Addr().String()

	// Test if we set httpCode to be 503 when certifier is down
	s, err := SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1+"/"+TEST_NETWORK_ID,
		"test cert string",
	)
	assert.NoError(t, err)
	assert.Equal(t, 503, s)
}

func TestAuthMiddleware(t *testing.T) {
	// set up auth middleware by creating root user, non-admin user bob, and their respective policies
	certifier_test_service.StartTestService(t)
	store := test_utils.GetCertifierBlobstore(t)
	bob := "bob"
	root := "root"
	bobUser, bobToken := test_utils.CreateTestUser(bob, "password")
	rootUser, rootToken := test_utils.CreateTestUser(root, "password")
	err := store.PutUser(bob, &bobUser)
	err = store.PutUser(root, &rootUser)
	bobPolicy := test_utils.CreateUserPolicy(t, bobToken)
	rootPolicy := test_utils.CreateAdminPolicy(rootToken)
	err = store.PutPolicy(bobToken, &bobPolicy)
	err = store.PutPolicy(rootToken, &rootPolicy)
	assert.NoError(t, err)

	e := startTestMiddlewareServer(t)
	e.Use(access.TokenMiddleware)
	listener := WaitForTestServer(t, e)
	if listener == nil {
		return
	}
	urlPrefix := "http://" + listener.Addr().String()
	testNetworkURLRoot := urlPrefix + "/magma/v1/networks"

	// Test READ endpoint
	s, err := SendRequestWithToken("GET", testNetworkURLRoot+obsidian.UrlSep+TEST_NETWORK_ID, root, rootToken)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)
	s, err = SendRequestWithToken("GET", testNetworkURLRoot+obsidian.UrlSep+TEST_NETWORK_ID, bob, bobToken)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Test WRITE endpoint
	s, err = SendRequestWithToken("PUT", testNetworkURLRoot+obsidian.UrlSep+TEST_NETWORK_ID, root, rootToken)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)
	s, err = SendRequestWithToken("PUT", testNetworkURLRoot+obsidian.UrlSep+TEST_NETWORK_ID, bob, bobToken)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)
}

func TestMiddleware(t *testing.T) {
	operCertSn, superCertSn := MockAccessControl(t)

	e := startTestMiddlewareServer(t)
	e.Use(access.CertificateMiddleware) // inject obsidian access control middleware
	listener := WaitForTestServer(t, e)

	if listener == nil {
		return // WaitForTestServer should have 'logged' error already
	}

	urlPrefix := "http://" + listener.Addr().String()

	// Test READ network entity
	s, err := SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1+"/"+TEST_NETWORK_ID,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Test WRITE network entity
	s, err = SendRequest(
		"PUT", // WRITE
		urlPrefix+RegisterNetworkV1+"/"+TEST_NETWORK_ID,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test READ network entity
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1+"/"+WRITE_TEST_NETWORK_ID,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test WRITE network entity
	s, err = SendRequest(
		"PUT", // WRITE
		urlPrefix+RegisterNetworkV1+"/"+WRITE_TEST_NETWORK_ID,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Test regular operator wildcard failures
	// Test READ network Wildcard
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test WRITE network Wildcard
	s, err = SendRequest(
		"POST", // WRITE
		urlPrefix+RegisterNetworkV1,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test WRITE Tenants URL
	s, err = SendRequest(
		"GET",
		urlPrefix+tenantsh.TenantInfoURL,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test WRITE Tenants URL
	s, err = SendRequest(
		"POST",
		urlPrefix+tenantsh.TenantInfoURL,
		operCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 403, s)

	// Test Supervisor Permissions
	// Super - Test READ network entity
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1+"/"+WRITE_TEST_NETWORK_ID,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE network entity
	s, err = SendRequest(
		"PUT", // WRITE
		urlPrefix+RegisterNetworkV1+"/"+TEST_NETWORK_ID,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test READ network Wildcard
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE network Wildcard
	s, err = SendRequest(
		"POST", // WRITE
		urlPrefix+RegisterNetworkV1,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test READ Any URL
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+RegisterNetworkV1,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE  Any URL
	s, err = SendRequest(
		"GET", // READ
		urlPrefix+"/malformed/url",
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE  Any URL
	s, err = SendRequest(
		"PUT", // WRITE
		urlPrefix+"/malformed/url",
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE Tenants URL
	s, err = SendRequest(
		"GET",
		urlPrefix+tenantsh.TenantInfoURL,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)

	// Super - Test WRITE Tenants URL
	s, err = SendRequest(
		"POST",
		urlPrefix+tenantsh.TenantInfoURL,
		superCertSn,
	)
	assert.NoError(t, err)
	assert.Equal(t, 200, s)
}

func startTestMiddlewareServer(t *testing.T) *echo.Echo {
	e := echo.New()
	assert.NotNil(t, e)

	dummyHandlerFunc := func(c echo.Context) error {
		return c.String(http.StatusOK, "All good!")
	}

	// Endpoint requiring Network Wildcard READ Access Permissions
	e.GET(RegisterNetworkV1, dummyHandlerFunc)

	// Endpoint requiring Network Wildcard WRITE Access Permissions
	e.POST(RegisterNetworkV1, dummyHandlerFunc)

	// Endpoint requiring a specific Network READ Entity Access Permissions
	e.GET(ManageNetworkV1, dummyHandlerFunc)

	// Endpoint requiring a specific Network WRITE Entity Access Permissions
	e.PUT(ManageNetworkV1, dummyHandlerFunc)

	// Endpoint requiring supervisor permissions
	e.GET("/malformed/url", dummyHandlerFunc)

	// Endpoint requiring Write supervisor permissions
	e.PUT("/malformed/url", dummyHandlerFunc)

	// Tenants Endpoint requiring Network Wildcard WRITE access permissions
	e.POST(tenantsh.TenantInfoURL, dummyHandlerFunc)

	// Tenants Endpoint requiring Network Wildcard READ access permissions
	e.GET(tenantsh.TenantInfoURL, dummyHandlerFunc)

	go func(t *testing.T) {
		assert.NoError(t, e.Start(""))
	}(t)

	return e
}
