// Package sesiontest es SOPORTE DE PRUEBAS. Contiene dobles y propuestas de
// Kiro no aprobadas que viven SOLO aquí (design.md §Soporte de pruebas).
//
// REGLA E (archtest): ningún paquete productivo puede importar sesiontest.
// Las propuestas de Kiro (HappyPathEmission, SnapshotXidLog, etc.) son SCAFFOLD
// para las pruebas de E4-T3 (R9, R10); se mueven a producción solo cuando su
// opción correspondiente (A-1, A-10, etc.) sea aprobada.
package sesiontest
