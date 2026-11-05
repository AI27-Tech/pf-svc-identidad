//go:build integration
// +build integration

package kc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TestSegundoFactorIncorrectoOVencido cubre la tarea 3.1 de E4-T5 (Requisitos
// 3.1, 3.2, 7.2) contra Keycloak 26.7.4 real: con contraseña válida, un código
// TOTP INCORRECTO y uno VENCIDO son rechazados con idp.ErrCredencialInvalida y
// Token cero, nunca como idp.ErrProveedorNoDisponible. Es la hermana de
// TestSinFactorRechazado (factor AUSENTE), que ya existía.
//
// Política OTP del realm: `realm-ai27.json` NO declara `otpPolicy`, así que
// rigen los valores por omisión de Keycloak 26.7.4 (periodo 30 s, ventana de
// tolerancia estrecha). El design (§3) autoriza, cuando no se lee la política
// efectiva, usar un desfase HOLGADO para el caso "vencido". Se usa un desfase de
// 10 periodos (300 s) hacia atrás: queda muy fuera de cualquier ventana de
// tolerancia por omisión, de modo que el rechazo es determinista sin depender
// del valor exacto de la política.
//
// Un único arranque de contenedor, dos subpruebas, como TestSinFactorRechazado.
func TestSegundoFactorIncorrectoOVencido(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)

	const usuario = "factory-monitorista"

	t.Run("codigo incorrecto", func(t *testing.T) {
		// Un código de 6 dígitos que NO es el vigente. codigoTOTPFresco da el
		// vigente; lo alteramos a uno garantizado distinto para no depender del
		// azar ni consumir la ventana vigente.
		incorrecto := codigoDistintoDelVigente()
		cred := idp.Credentials{
			Usuario:       usuario,
			Contrasena:    contrasenaFactory,
			SegundoFactor: incorrecto,
		}
		tok, err := prov.Issue(context.Background(), cred, uuid.NewString())

		if err == nil {
			t.Fatal("Issue aceptó un código de segundo factor incorrecto")
		}
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			t.Errorf("código incorrecto: esperaba ErrCredencialInvalida, obtuvo %v", err)
		}
		if errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("código incorrecto no debe ser ErrProveedorNoDisponible: %v", err)
		}
		if tok.AccessToken != "" || tok.RefreshToken != "" {
			t.Error("código incorrecto devolvió Token no cero")
		}
	})

	t.Run("codigo vencido", func(t *testing.T) {
		// Código TOTP válido PERO calculado para un instante 10 periodos (300 s)
		// en el pasado: fuera de la ventana de tolerancia del realm. Desfase
		// holgado por diseño (ver encabezado), no asume el valor exacto de la
		// política OTP.
		instanteVencido := time.Now().Add(-10 * periodoTOTP)
		vencido := totpAhora(semillaTOTP, instanteVencido)
		cred := idp.Credentials{
			Usuario:       usuario,
			Contrasena:    contrasenaFactory,
			SegundoFactor: vencido,
		}
		tok, err := prov.Issue(context.Background(), cred, uuid.NewString())

		if err == nil {
			t.Fatal("Issue aceptó un código de segundo factor vencido")
		}
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			t.Errorf("código vencido: esperaba ErrCredencialInvalida, obtuvo %v", err)
		}
		if errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("código vencido no debe ser ErrProveedorNoDisponible: %v", err)
		}
		if tok.AccessToken != "" || tok.RefreshToken != "" {
			t.Error("código vencido devolvió Token no cero")
		}
	})
}

// codigoDistintoDelVigente devuelve un código de 6 dígitos garantizado distinto
// del TOTP vigente para la semilla de prueba, sin consumir la ventana vigente
// (no usa codigoTOTPFresco, que marca la ventana como usada). Calcula el vigente
// y, si coincide, lo desplaza en un dígito.
func codigoDistintoDelVigente() string {
	vigente := totpAhora(semillaTOTP, time.Now())
	const fijo = "000000"
	if vigente != fijo {
		return fijo
	}
	return "111111"
}
