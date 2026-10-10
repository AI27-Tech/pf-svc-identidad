//go:build integration
// +build integration

package kc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// imagenKeycloak es la imagen del contenedor de pruebas.
//
// A-7 (provisional): la suite corre contra 26.7.4 exacta, fijada por la
// respuesta de PM a las decisiones C/D (2026-10-09, punto 5), en la línea de
// DEC-070. Nunca se usa `latest`. DEC-081 sigue su curso para el resto del
// endurecimiento; cuando fije la versión estable definitiva se actualiza aquí.
const imagenKeycloak = "quay.io/keycloak/keycloak:26.7.4"

// Constantes de prueba compartidas con testdata/realm-ai27-users-0.json. Las
// contraseñas son constantes de prueba, válidas sólo en el Keycloak_De_Prueba.
const (
	contrasenaFactory = "Factory-Pass-1!"
	// semillaTOTP es el secreto TOTP compartido por los usuarios factory-* y
	// cambio-rol en el archivo de usuarios (secretData.value). factorPara deriva
	// de aquí el código TOTP vigente (RFC 6238).
	//
	// Al importar una credencial OTP, Keycloak usa secretData.value VERBATIM
	// como clave HMAC —los bytes crudos de la cadena, sin decodificar base32—.
	// (El valor base32 que ve el usuario en el QR es la codificación de esos
	// bytes, no lo que se guarda.) Por eso la semilla es una cadena ASCII
	// imprimible y totpAhora la usa tal cual, sin base32.DecodeString: así el
	// test calcula el código con la misma clave que Keycloak.
	semillaTOTP = "ai27-totp-secret"
)

// kcDePrueba agrupa todo lo necesario para hablar con el Keycloak del
// contenedor durante la prueba.
type kcDePrueba struct {
	baseURL     string // p. ej. http://localhost:32768
	adminUser   string
	adminPass   string
	loginSecret string
	svcSecret   string
	httpClient  *http.Client
}

// tenantPorUsuario replica los tenant_id fijos de testdata para construir el
// ModeloVigente esperado por usuario (Propiedad 13).
var tenantPorUsuario = map[string]string{
	"factory-cliente":        "aaaa0000-0000-0000-0000-000000000001",
	"factory-jefe_operativo": "aaaa0000-0000-0000-0000-000000000002",
	"factory-monitorista":    "aaaa0000-0000-0000-0000-000000000003",
	"factory-supervisor":     "aaaa0000-0000-0000-0000-000000000004",
	"factory-coordinador":    "aaaa0000-0000-0000-0000-000000000005",
	"factory-ejecutivo":      "aaaa0000-0000-0000-0000-000000000006",
	"factory-reaccion":       "aaaa0000-0000-0000-0000-000000000007",
	"factory-administrador":  "aaaa0000-0000-0000-0000-000000000008",
}

// subPorUsuario replica el id fijo de cada usuario factory-* del archivo de
// usuarios. Por DEC-072 opción (c), el `sub` del token es el id opaco de
// Keycloak, que aquí es conocido porque el import fija el id del usuario.
var subPorUsuario = map[string]string{
	"factory-cliente":        "11111111-0000-0000-0000-000000000001",
	"factory-jefe_operativo": "11111111-0000-0000-0000-000000000002",
	"factory-monitorista":    "11111111-0000-0000-0000-000000000003",
	"factory-supervisor":     "11111111-0000-0000-0000-000000000004",
	"factory-coordinador":    "11111111-0000-0000-0000-000000000005",
	"factory-ejecutivo":      "11111111-0000-0000-0000-000000000006",
	"factory-reaccion":       "11111111-0000-0000-0000-000000000007",
	"factory-administrador":  "11111111-0000-0000-0000-000000000008",
}

// modeloDe construye el ModeloVigente esperado para un usuario factory-<rol>.
func modeloDe(usuario string, rol autorizacion.Rol) autorizacion.ModeloVigente {
	return autorizacion.ModeloVigente{
		SujetoID: subPorUsuario[usuario],
		TenantID: tenantPorUsuario[usuario],
		Rol:      rol,
	}
}

// esperadoSujeto devuelve el valor esperado del claim `sub` para un modelo.
//
// A-13 (DEC-072, opción c): el `sub` del token es el identificador opaco del
// IdP (el id del usuario de Keycloak), y el SujetoID de AI27 se resuelve en
// servidor por IdpSubject. En las pruebas el id de Keycloak se fija en el
// archivo de usuarios, de modo que `sub == SujetoID` por construcción del
// fixture; en producción esa igualdad no se asume (la resuelve E4-T3/E4-T6).
// Concentrar aquí el valor esperado deja el diseño listo para otra opción de
// A-13 sin tocar las aserciones.
func esperadoSujeto(m autorizacion.ModeloVigente) string {
	return m.SujetoID
}

// periodoTOTP es el periodo de la ventana TOTP del artefacto (30 s, RFC 6238).
const periodoTOTP = 30 * time.Second

// margenTOTP es el colchón mínimo de vida que debe quedarle a la ventana actual
// para usarla. Si queda menos, se espera a la ventana siguiente antes de emitir:
// así el código no expira entre que lo calculamos y que Keycloak lo valida
// (arranque del contenedor + red). Evita el parpadeo por el borde de ventana.
const margenTOTP = 3 * time.Second

// ventanaTOTP serializa el consumo de códigos TOTP entre todas las emisiones de
// la prueba para que cada una use un código de una ventana distinta. Keycloak,
// por RFC 6238 §5.2 y su política OTP por omisión (otpPolicyCodeReusable=false),
// rechaza un código TOTP ya usado en su ventana; como todos los usuarios de
// prueba comparten la misma semilla, dos emisiones en los mismos 30 s
// presentarían el mismo código y la segunda fallaría con credencial inválida.
var ventanaTOTP struct {
	mu            sync.Mutex
	ultimaUsadaID int64 // número de ventana (unix/30) ya consumido
}

// codigoTOTPFresco devuelve un código TOTP garantizado como no reutilizado: si
// la ventana actual ya se consumió, o le queda menos que margenTOTP, espera al
// inicio de la siguiente y usa esa. Serializa con un mutex porque la semilla es
// compartida. Es asunto de la prueba (una app real recibe el OTP del usuario y
// no lo recalcula); por eso vive aquí y no en el adaptador.
func codigoTOTPFresco() string {
	ventanaTOTP.mu.Lock()
	defer ventanaTOTP.mu.Unlock()

	for {
		ahora := time.Now()
		ventanaID := ahora.Unix() / int64(periodoTOTP/time.Second)
		restante := periodoTOTP - time.Duration(ahora.UnixNano())%periodoTOTP
		yaUsada := ventanaID == ventanaTOTP.ultimaUsadaID
		if !yaUsada && restante >= margenTOTP {
			ventanaTOTP.ultimaUsadaID = ventanaID
			return totpAhora(semillaTOTP, ahora)
		}
		// Ventana ya consumida o sin margen: esperar al inicio de la siguiente.
		time.Sleep(restante)
	}
}

// factorPara devuelve un segundo factor (código TOTP) fresco para un usuario.
//
// A-3 (DEC-071): el segundo factor de V1 es TOTP nativo de Keycloak. Todos los
// usuarios factory-* comparten la misma semilla de prueba, así que el código se
// calcula igual para todos; codigoTOTPFresco evita reutilizar un mismo código en
// su ventana. RFC 6238 con HMAC-SHA1, 6 dígitos y periodo de 30 s.
func factorPara(usuario string) string {
	return codigoTOTPFresco()
}

// emitirConTOTP emite un token para un usuario con OTP usando un código fresco
// (no reutilizado) de una ventana con margen suficiente. No enmascara errores:
// cualquier fallo del Issue se devuelve tal cual.
func emitirConTOTP(t *testing.T, prov idp.IdentityProvider, usuario string) (idp.Token, error) {
	t.Helper()
	cred := idp.Credentials{
		Usuario:       usuario,
		Contrasena:    contrasenaFactory,
		SegundoFactor: codigoTOTPFresco(),
	}
	return prov.Issue(context.Background(), cred)
}

// totpAhora calcula el código TOTP (RFC 6238) para un secreto y un instante
// dados: HMAC-SHA1, 6 dígitos, periodo de 30 s.
//
// La clave HMAC son los bytes crudos del secreto, no una decodificación base32:
// así coincide con cómo Keycloak usa secretData.value en una credencial OTP
// importada (ver semillaTOTP).
func totpAhora(secreto string, t time.Time) string {
	clave := []byte(secreto)
	contador := uint64(t.Unix()) / 30
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], contador)

	mac := hmac.New(sha1.New, clave)
	mac.Write(buf[:])
	suma := mac.Sum(nil)

	offset := suma[len(suma)-1] & 0x0f
	binario := (uint32(suma[offset]&0x7f) << 24) |
		(uint32(suma[offset+1]) << 16) |
		(uint32(suma[offset+2]) << 8) |
		uint32(suma[offset+3])
	codigo := binario % 1000000
	return fmt.Sprintf("%06d", codigo)
}

// arranqueKeycloak levanta el contenedor de Keycloak con los dos JSON de
// import, el admin bootstrap y secretos aleatorios. Si Docker no está
// disponible hace t.Skip; cualquier error posterior es t.Fatalf con la etapa.
func arranqueKeycloak(t *testing.T) kcDePrueba {
	t.Helper()

	ctx := context.Background()

	// R11.4: la ÚNICA condición de omisión es que Docker no esté disponible.
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("Docker no disponible: %v", err)
	}
	if err := provider.Health(ctx); err != nil {
		t.Skipf("Docker no disponible: %v", err)
	}

	adminPass := secretoAleatorio(t)
	loginSecret := secretoAleatorio(t)
	svcSecret := secretoAleatorio(t)

	realmFile := filepath.Join(repoRoot(t), "deploy", "keycloak", "realm-ai27.json")
	usersFile := filepath.Join(repoRoot(t), "internal", "idp", "internal", "kc", "testdata", "realm-ai27-users-0.json")

	req := testcontainers.ContainerRequest{
		Image:        imagenKeycloak,
		ExposedPorts: []string{"8080/tcp"},
		Cmd:          []string{"start-dev", "--import-realm"},
		Env: map[string]string{
			"KC_BOOTSTRAP_ADMIN_USERNAME": "admin",
			"KC_BOOTSTRAP_ADMIN_PASSWORD": adminPass,
			"KC_AI27_LOGIN_SECRET":        loginSecret,
			"KC_AI27_SVC_SECRET":          svcSecret,
		},
		// Convención de nombres del import por directorio de Keycloak: el realm
		// va en <realm>-realm.json y sus usuarios en <realm>-users-<n>.json.
		// Con cualquier otro nombre (p. ej. realm-ai27-users-0.json) Keycloak
		// importa el realm pero IGNORA los usuarios, y toda emisión de token
		// falla con "user_not_found". Los archivos del repo conservan su nombre;
		// aquí se montan con el nombre que Keycloak espera.
		Files: []testcontainers.ContainerFile{
			{HostFilePath: realmFile, ContainerFilePath: "/opt/keycloak/data/import/ai27-realm.json", FileMode: 0o644},
			{HostFilePath: usersFile, ContainerFilePath: "/opt/keycloak/data/import/ai27-users-0.json", FileMode: 0o644},
		},
		WaitingFor: wait.ForHTTP("/realms/ai27/.well-known/openid-configuration").
			WithPort("8080/tcp").
			WithStartupTimeout(120 * time.Second),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	// Registrar la limpieza justo después de crear el contenedor (R11.8).
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("arranque: no se pudo iniciar Keycloak: %v", err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("arranque: host del contenedor: %v", err)
	}
	puerto, err := ctr.MappedPort(ctx, "8080")
	if err != nil {
		t.Fatalf("arranque: puerto del contenedor: %v", err)
	}

	base := fmt.Sprintf("http://%s:%s", host, puerto.Port())

	kc := kcDePrueba{
		baseURL:     base,
		adminUser:   "admin",
		adminPass:   adminPass,
		loginSecret: loginSecret,
		svcSecret:   svcSecret,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}

	// Verificar que el realm ai27 quedó importado (etapa import).
	resp, err := kc.httpClient.Get(base + "/realms/ai27/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("import: el realm ai27 no respondió: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("import: el discovery del realm ai27 respondió %d", resp.StatusCode)
	}

	return kc
}

// tokenURL devuelve el endpoint de token del realm ai27.
func (k kcDePrueba) tokenURL() string {
	return k.baseURL + "/realms/ai27/protocol/openid-connect/token"
}

// adminSessionsURL devuelve la base de administración de sesiones del realm.
func (k kcDePrueba) adminSessionsURL() string {
	return k.baseURL + "/admin/realms/ai27/sessions"
}

// jwksURL devuelve el endpoint JWKS del realm ai27.
func (k kcDePrueba) jwksURL() string {
	return k.baseURL + "/realms/ai27/protocol/openid-connect/certs"
}

// configAdaptador arma la Config del adaptador contra el contenedor.
func (k kcDePrueba) configAdaptador() Config {
	return Config{
		TokenURL:            k.tokenURL(),
		RefreshURL:          k.tokenURL(),
		AdminSessionsURL:    k.adminSessionsURL(),
		ClientID:            "ai27-login",
		ClientSecret:        k.loginSecret,
		ServiceClientID:     "ai27-identidad-svc",
		ServiceClientSecret: k.svcSecret,
		Timeout:             15 * time.Second,
	}
}

// adaptador construye el adaptador real contra el contenedor.
func (k kcDePrueba) adaptador(t *testing.T) idp.IdentityProvider {
	t.Helper()
	a, err := New(k.configAdaptador())
	if err != nil {
		t.Fatalf("New(adaptador): %v", err)
	}
	return a
}

// factory es la Factory de idptest contra Keycloak: devuelve el adaptador y una
// credencial válida de factory-monitorista con su OTP vigente (R1.6). Espera a
// una ventana TOTP con margen para que el código no expire mientras la suite de
// contrato lo usa (la suite llama Issue por su cuenta, sin el reintento de
// emitirConTOTP).
func (k kcDePrueba) factory() func(t *testing.T) (idp.IdentityProvider, idp.Credentials) {
	return func(t *testing.T) (idp.IdentityProvider, idp.Credentials) {
		return k.adaptador(t), idp.Credentials{
			Usuario:       "factory-monitorista",
			Contrasena:    contrasenaFactory,
			SegundoFactor: codigoTOTPFresco(),
		}
	}
}

// tokenAdmin obtiene un access token del admin del realm master por ROPC con el
// cliente admin-cli. Se usa para las operaciones administrativas de la prueba
// (lectura de grupos, cambio de grupo, control negativo de permisos).
func (k kcDePrueba) tokenAdmin(t *testing.T) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", "admin-cli")
	form.Set("username", k.adminUser)
	form.Set("password", k.adminPass)

	resp, err := k.httpClient.PostForm(k.baseURL+"/realms/master/protocol/openid-connect/token", form)
	if err != nil {
		t.Fatalf("token admin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("token admin: respondió %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("token admin: decodificar: %v", err)
	}
	return tr.AccessToken
}

// tokenServicio obtiene un access token de la cuenta de servicio ai27-identidad-svc.
func (k kcDePrueba) tokenServicio(t *testing.T) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "ai27-identidad-svc")
	form.Set("client_secret", k.svcSecret)

	resp, err := k.httpClient.PostForm(k.tokenURL(), form)
	if err != nil {
		t.Fatalf("token servicio: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("token servicio: respondió %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("token servicio: decodificar: %v", err)
	}
	return tr.AccessToken
}

// peticionAdmin ejecuta una petición a la API de administración del realm ai27
// con el bearer dado y devuelve el status y el cuerpo.
func (k kcDePrueba) peticionAdmin(t *testing.T, metodo, ruta, bearer string, cuerpo []byte) (int, []byte) {
	t.Helper()
	var body *strings.Reader
	if cuerpo != nil {
		body = strings.NewReader(string(cuerpo))
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(metodo, k.baseURL+"/admin/realms/ai27"+ruta, body)
	if err != nil {
		t.Fatalf("peticionAdmin %s %s: %v", metodo, ruta, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.httpClient.Do(req)
	if err != nil {
		t.Fatalf("peticionAdmin %s %s: %v", metodo, ruta, err)
	}
	defer resp.Body.Close()
	datos := leerTodo(resp)
	return resp.StatusCode, datos
}

func leerTodo(resp *http.Response) []byte {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}

// secretoAleatorio genera un secreto hex aleatorio con crypto/rand.
func secretoAleatorio(t *testing.T) string {
	t.Helper()
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return fmt.Sprintf("%x", b[:])
}
