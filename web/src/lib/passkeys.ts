// Passkeys in the browser (docs/access.md#passkeys): the server sends the
// options for navigator.credentials, binary values base64url, and is sent
// back what the passkey answered, the same way.

/** A passkey's answer to a challenge, as the server takes it. */
export interface PasskeyAssertion {
  id: string;
  client_data_json: string;
  authenticator_data: string;
  signature: string;
}

/** Whether this browser can use passkeys at all. */
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && 'PublicKeyCredential' in window && !!navigator.credentials;
}

export function fromBase64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

export function toBase64url(buf: ArrayBuffer): string {
  let bin = '';
  for (const b of new Uint8Array(buf)) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

type Described = { type: 'public-key'; id: string };

/** The server's options for navigator.credentials.create. */
export interface CreationOptions {
  challenge: string;
  user: { id: string; name: string; displayName: string };
  excludeCredentials?: Described[];
  [option: string]: unknown;
}

/** The server's options for navigator.credentials.get. */
export interface RequestOptions {
  challenge: string;
  allowCredentials?: Described[];
  [option: string]: unknown;
}

const described = (list: Described[] | undefined) => (list ?? []).map((c) => ({ ...c, id: fromBase64url(c.id) }));

/** Says why a passkey did not answer, in words for the person. */
export function passkeyMessage(err: unknown): string {
  if (err instanceof DOMException) {
    if (err.name === 'NotAllowedError') return 'The passkey did not answer: it was cancelled, or took too long. Try again.';
    if (err.name === 'InvalidStateError') return 'That passkey is registered already.';
    if (err.name === 'SecurityError') return 'This browser refused the passkey for this address. Use BAM! at the address it was set up at.';
  }
  return err instanceof Error ? err.message : 'The passkey did not answer.';
}

/** Registers a passkey: the browser makes one from the server's options. */
export async function createPasskey(options: CreationOptions): Promise<{ client_data_json: string; attestation_object: string }> {
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...options,
      challenge: fromBase64url(options.challenge),
      user: { ...options.user, id: fromBase64url(options.user.id) },
      excludeCredentials: described(options.excludeCredentials),
    } as unknown as PublicKeyCredentialCreationOptions,
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('No passkey was made.');
  const res = cred.response as AuthenticatorAttestationResponse;
  return { client_data_json: toBase64url(res.clientDataJSON), attestation_object: toBase64url(res.attestationObject) };
}

/** Asks one of the person's passkeys to answer the server's challenge. */
export async function getPasskey(options: RequestOptions): Promise<PasskeyAssertion> {
  const cred = (await navigator.credentials.get({
    publicKey: {
      ...options,
      challenge: fromBase64url(options.challenge),
      allowCredentials: described(options.allowCredentials),
    } as unknown as PublicKeyCredentialRequestOptions,
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('No passkey answered.');
  const res = cred.response as AuthenticatorAssertionResponse;
  return {
    id: toBase64url(cred.rawId),
    client_data_json: toBase64url(res.clientDataJSON),
    authenticator_data: toBase64url(res.authenticatorData),
    signature: toBase64url(res.signature),
  };
}
