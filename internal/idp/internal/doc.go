// Package internal alberga el adaptador CONCRETO del IdP on-premise con OIDC.
//
// El IdP de V1 es Keycloak (confirmado 2026-10-08, pendiente de asentar en
// decision-log). El adaptador vive bajo este árbol:
//
//   - internal/idp/internal/kc: la implementación contra Keycloak (configuración,
//     clasificación de respuestas, Issue/Refresh/Revoke, token de servicio,
//     telemetría y probe de alcanzabilidad). Es el ÚNICO lugar del módulo que
//     conoce el protocolo OIDC y los endpoints del proveedor (restricción del
//     scaffold, §A.5 R2, verificada por internal/archtest).
//   - internal/idp/keycloak: la fachada pública que la raíz de composición
//     (cmd/api) importa para construir el adaptador. No contiene lógica ni tipos
//     de terceros; sólo reexporta New, Probe y el tipo Config.
//
// El adaptador implementa idp.IdentityProvider y pasa la suite de contrato
// idp/idptest sin cambios. Prohibido importar cualquier SDK de AWS (DEC-062):
// V1 no usa AWS ni Cognito.
package internal
