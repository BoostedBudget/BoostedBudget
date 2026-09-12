/** Build/test-only catalog validation. Accepts parsed OpenAPI; no Worker I/O dependencies. */
import { responseCatalog, responseProfiles, type ProjectionTarget } from './projection'
import { getCapability } from '../manifest/capability-manifest'

export interface ResponseSchema {
  $ref?: string
  type?: string | string[]
  properties?: Record<string, ResponseSchema>
  items?: ResponseSchema
  required?: string[]
  [key: string]: unknown
}

export interface ResponseOpenAPI {
  components?: { schemas?: Record<string, ResponseSchema> }
  paths?: Record<string, Record<string, unknown>>
  [key: string]: unknown
}

export function resolveResponseSchema(schema: ResponseSchema | undefined, spec: ResponseOpenAPI): ResponseSchema | undefined {
  const seen = new Set<string>()
  while (schema?.$ref) {
    if (seen.has(schema.$ref)) return undefined
    seen.add(schema.$ref)
    schema = spec.components?.schemas?.[schema.$ref.replace('#/components/schemas/', '')]
  }
  return schema
}

export function validateResponseCatalog(spec: ResponseOpenAPI, exposedOperationIds: readonly string[]): string[] {
  const errors: string[] = []
  const exposed = new Set(exposedOperationIds)
  for (const operationId of exposed) if (!responseCatalog[operationId]) errors.push(`${operationId}: missing response policy`)
  for (const operationId of Object.keys(responseCatalog)) if (!exposed.has(operationId)) errors.push(`${operationId}: stale response policy`)

  for (const [name, profile] of Object.entries(responseProfiles)) {
    const schema = resolveResponseSchema(spec.components?.schemas?.[profile.schema], spec)
    if (!schema?.properties) { errors.push(`${name}: missing object schema ${profile.schema}`); continue }
    const decisions = [...profile.keep, ...profile.omit]
    const known = new Set(decisions)
    if (known.size !== decisions.length) errors.push(`${name}: duplicate field policy`)
    for (const field of Object.keys(schema.properties)) if (!known.has(field)) errors.push(`${name}: unclassified field ${field}`)
    for (const field of known) if (!(field in schema.properties)) errors.push(`${name}: stale field ${field}`)
    for (const [field, child] of Object.entries(profile.children)) {
      if (!profile.keep.includes(field)) errors.push(`${name}.${field}: child must be retained`)
      validateTarget(schema.properties[field], child, `${name}.${field}`)
      if (child.required && !schema.required?.includes(field)) errors.push(`${name}.${field}: optional schema field marked required`)
    }
  }

  function validateTarget(schema: ResponseSchema | undefined, target: ProjectionTarget, location: string): void {
    let resolved = resolveResponseSchema(schema, spec)
    if (target.array) {
      if (!resolved?.items) { errors.push(`${location}: expected array schema`); return }
      resolved = resolveResponseSchema(resolved.items, spec)
    }
    const profile = responseProfiles[target.profile]
    const expected = profile && resolveResponseSchema(spec.components?.schemas?.[profile.schema], spec)
    if (!resolved?.properties || !expected?.properties) { errors.push(`${location}: missing target object ${target.profile}`); return }
    // Object identity works for dereferenced schemas; structural comparison also handles raw refs
    // and synthetic build fixtures without coupling to a particular dereferencer.
    if (JSON.stringify(resolved) !== JSON.stringify(expected)) errors.push(`${location}: response schema differs from ${target.profile}`)
  }

  const seenOperations = new Set<string>()
  const operationMethods = new Map<string, string>()
  for (const path of Object.values(spec.paths ?? {})) {
    for (const [method, raw] of Object.entries(path)) {
      if (!['get', 'post', 'put', 'patch', 'delete', 'head', 'options'].includes(method) || typeof raw !== 'object' || raw === null) continue
      const operation = raw as { operationId?: string; responses?: Record<string, { content?: Record<string, { schema?: ResponseSchema }> }> }
      if (!operation.operationId || !exposed.has(operation.operationId)) continue
      seenOperations.add(operation.operationId)
      operationMethods.set(operation.operationId, method)
      const policy = responseCatalog[operation.operationId]
      if (policy?.kind !== 'project') continue
      if (!exposed.has(policy.recoveryOperationId)) errors.push(`${operation.operationId}: recovery operation is not exposed`)
      let successfulSchema = false
      for (const [status, response] of Object.entries(operation.responses ?? {})) {
        if (!/^2\d\d$/.test(status)) continue
        const schema = response.content?.['application/json']?.schema
        if (schema) { validateTarget(schema, policy.target, operation.operationId); successfulSchema = true }
      }
      if (!successfulSchema) errors.push(`${operation.operationId}: no successful JSON response schema`)
    }
  }
  const recoveryOperations = new Set(Object.values(responseCatalog).flatMap((policy) => policy.kind === 'project' ? [policy.recoveryOperationId] : []))
  for (const operationId of recoveryOperations) {
    const capability = getCapability(operationId)
    if (operationMethods.get(operationId) !== 'get' || capability?.class !== 'read' || capability.excluded) {
      errors.push(`${operationId}: recovery must be an admitted GET read`)
    }
  }
  for (const operationId of exposed) if (!seenOperations.has(operationId)) errors.push(`${operationId}: missing OpenAPI operation`)
  return errors
}
