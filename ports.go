package identidad

// ports.go declara los PUERTOS DE SALIDA que este módulo necesita como
// consumidor de otros módulos de AI27.
//
// `identidad` NO tiene ninguno: es HOJA del grafo de arquitectura
// (pf-pkg-platform-go/tools/arch-test/reglas.yaml: `identidad: []`). No consume
// a ningún otro `pf-svc-*` en proceso ni por HTTP.
//
// El IdP on-premide del que depende la emisión y firma de tokens es una
// dependencia EXTERNA, no una arista de AI27: por eso no aparece en reglas.yaml
// (identidad.md (f)) y su puerto vive aislado en internal/idp, no aquí. Las
// decisiones de autorización por petición son de cada módulo consumidor, no de
// `identidad` (identidad.md (a)).
//
// Este archivo se deja intencionalmente sin interfaces de salida. Si en el
// futuro `identidad` adquiriera una arista saliente hacia otro módulo de AI27,
// su puerto se declararía aquí (público, para que la raíz de composición pueda
// implementarlo), según la regla 2 de 00-indice.md.
