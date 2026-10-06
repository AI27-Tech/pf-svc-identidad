// Package internal albergará el adaptador CONCRETO del IdP on-premise con OIDC.
//
// EN ESPERA (ADR 32, en Propuesta; DEC-062, Impacto). No hay código aquí
// todavía y no debe haberlo hasta que ADR 32 elija el componente (propuesta:
// Keycloak, o el IdP corporativo). Cuando lo haga, este paquete:
//
//   - implementará idp.IdentityProvider contra el IdP elegido;
//   - será el ÚNICO lugar del módulo que importe el SDK, cliente OIDC o API de
//     administración del proveedor (restricción del scaffold, §A.5 R2);
//   - deberá pasar la suite de contrato idp/idptest sin cambios.
//
// TODO(ADR 32): elegir componente, implementar el adaptador y su empaquetado
// para el orquestador on-premise (propuesta ADR 32: k3s). Prohibido importar
// cualquier SDK de AWS (DEC-062): V1 no usa AWS ni Cognito.
package internal
