package tech.ai27.keycloak;

import jakarta.ws.rs.core.MultivaluedMap;
import org.keycloak.authentication.AuthenticationFlowContext;
import org.keycloak.authentication.AuthenticationFlowError;
import org.keycloak.authentication.Authenticator;
import org.keycloak.models.KeycloakSession;
import org.keycloak.models.RealmModel;
import org.keycloak.models.UserModel;

/**
 * SesionIdNoteAuthenticator transporta el {@code sesion_id} de AI27 hacia la
 * sesión de Keycloak (DEC-083).
 *
 * <p>Corre como ejecutor REQUIRED del flujo {@code ai27-direct-grant}, después
 * de validar usuario, contraseña y OTP. Lee el parámetro de formulario
 * {@code sesion_id} —que GENERA {@code identidad} (E4-T3), no el sujeto— y lo
 * guarda como nota de la sesión de autenticación con la clave {@code SESION_ID}.
 * El note-mapper del realm ({@code oidc-usersessionmodel-note-mapper}) emite esa
 * nota como el claim {@code sesion_id} en el access token, y la nota sobrevive a
 * los refrescos, por lo que el {@code sesion_id} se mantiene estable (ADR 26 §1).
 *
 * <p>Falla cerrada (DEC-083 nota 2): si el parámetro falta o no es un UUID
 * válido, rechaza la autenticación en vez de emitir un token sin
 * {@code sesion_id}.
 */
public class SesionIdNoteAuthenticator implements Authenticator {

    /** Nombre del parámetro de formulario que lleva el sesion_id de AI27. */
    static final String PARAM_SESION_ID = "sesion_id";

    /**
     * Clave de la nota de sesión de usuario. Debe coincidir EXACTAMENTE con
     * {@code user.session.note} del note-mapper en el realm (SESION_ID).
     */
    static final String NOTA_SESION_ID = "SESION_ID";

    @Override
    public void authenticate(AuthenticationFlowContext context) {
        MultivaluedMap<String, String> formParams =
                context.getHttpRequest().getDecodedFormParameters();
        String sesionID = formParams != null ? formParams.getFirst(PARAM_SESION_ID) : null;

        if (sesionID == null || !esUUIDValido(sesionID)) {
            // Falla cerrada: sin un sesion_id válido no se emite el token. El
            // error se mapea a credencial inválida para no revelar el motivo.
            context.failure(AuthenticationFlowError.INVALID_CREDENTIALS);
            return;
        }

        // La nota se escribe en la sesión de autenticación, que Keycloak
        // promueve a sesión de usuario; de ahí la lee el note-mapper.
        context.getAuthenticationSession().setUserSessionNote(NOTA_SESION_ID, sesionID);
        context.success();
    }

    /**
     * esUUIDValido verifica que el valor sea un UUID canónico. Se evita
     * {@code java.util.UUID.fromString} como única validación porque es
     * tolerante con formas no canónicas; aquí se exige el formato exacto que
     * genera {@code identidad}.
     */
    private static boolean esUUIDValido(String valor) {
        if (valor.length() != 36) {
            return false;
        }
        for (int i = 0; i < 36; i++) {
            char c = valor.charAt(i);
            if (i == 8 || i == 13 || i == 18 || i == 23) {
                if (c != '-') {
                    return false;
                }
            } else if (Character.digit(c, 16) < 0) {
                return false;
            }
        }
        return true;
    }

    @Override
    public void action(AuthenticationFlowContext context) {
        // Sin interacción con el usuario: todo ocurre en authenticate.
    }

    @Override
    public boolean requiresUser() {
        return false;
    }

    @Override
    public boolean configuredFor(KeycloakSession session, RealmModel realm, UserModel user) {
        return true;
    }

    @Override
    public void setRequiredActions(KeycloakSession session, RealmModel realm, UserModel user) {
        // No agrega acciones requeridas.
    }

    @Override
    public void close() {
        // Sin recursos que liberar.
    }
}
