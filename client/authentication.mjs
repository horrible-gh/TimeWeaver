export function availableLoginMethods(response) {
  return Array.isArray(response?.methods) && response.methods.includes('os') ? ['local', 'os'] : ['local'];
}

export function loginPayload(username, password, method = 'local', sessionMode = true) {
  if (!sessionMode) return { username, password };
  if (method !== 'local' && method !== 'os') throw new Error('invalid_auth_method');
  return { username, password, method };
}

// OS identity is resolved by the server. The browser never submits a subject,
// native ID, OS password or generated application ID when creating a binding.
export function userPayload(form, groups, create) {
  const method = form.auth_method || 'local';
  if (method !== 'local' && method !== 'os') throw new Error('invalid_auth_method');
  const body = { name: form.name, email: form.email, role: form.role, status: form.status, groups, version: form.version, auth_method: method };
  if (!create || method === 'local') body.user_id = form.user_id;
  if (method === 'os') body.os_name = (form.os_name || '').trim();
  else if (create) body.password = form.password;
  return body;
}
