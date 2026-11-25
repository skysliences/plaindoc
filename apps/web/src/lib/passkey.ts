import type { PasskeyAssertion, PasskeyAttestation } from "../data-access";
import { formatError } from "../editor/status-utils";

export function supportsPasskeys(): boolean {
  return typeof window !== "undefined" &&
    window.isSecureContext &&
    typeof PublicKeyCredential !== "undefined" &&
    typeof navigator.credentials?.get === "function" &&
    typeof navigator.credentials?.create === "function";
}

function fromBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const bytes = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(bytes, (byte) => byte.charCodeAt(0));
}

function toBase64Url(value: ArrayBuffer): string {
  let bytes = "";
  for (const byte of new Uint8Array(value)) {
    bytes += String.fromCharCode(byte);
  }
  return btoa(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function decodeDescriptor(descriptor: PublicKeyCredentialDescriptorJSON): PublicKeyCredentialDescriptor {
  return {
    type: "public-key",
    id: fromBase64Url(descriptor.id),
    transports: descriptor.transports as AuthenticatorTransport[] | undefined
  };
}

function requestOptions(options: PublicKeyCredentialRequestOptionsJSON): PublicKeyCredentialRequestOptions {
  if (typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function") {
    return PublicKeyCredential.parseRequestOptionsFromJSON(options);
  }
  return {
    challenge: fromBase64Url(options.challenge),
    allowCredentials: options.allowCredentials?.map(decodeDescriptor),
    rpId: options.rpId,
    timeout: options.timeout,
    userVerification: options.userVerification as UserVerificationRequirement | undefined
  };
}

function creationOptions(options: PublicKeyCredentialCreationOptionsJSON): PublicKeyCredentialCreationOptions {
  if (typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function") {
    return PublicKeyCredential.parseCreationOptionsFromJSON(options);
  }
  return {
    rp: options.rp,
    user: { ...options.user, id: fromBase64Url(options.user.id) },
    challenge: fromBase64Url(options.challenge),
    pubKeyCredParams: options.pubKeyCredParams,
    timeout: options.timeout,
    excludeCredentials: options.excludeCredentials?.map(decodeDescriptor),
    authenticatorSelection: options.authenticatorSelection,
    attestation: options.attestation as AttestationConveyancePreference | undefined
  };
}

function credentialFields(credential: PublicKeyCredential) {
  return {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults()
  };
}

export async function getPasskeyAssertion(options: PublicKeyCredentialRequestOptionsJSON): Promise<PasskeyAssertion> {
  const credential = await navigator.credentials.get({ publicKey: requestOptions(options) });
  if (!(credential instanceof PublicKeyCredential) || !("signature" in credential.response)) {
    throw new Error("浏览器未返回有效的 Passkey 验证结果");
  }
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    ...credentialFields(credential),
    response: {
      clientDataJSON: toBase64Url(response.clientDataJSON),
      authenticatorData: toBase64Url(response.authenticatorData),
      signature: toBase64Url(response.signature),
      userHandle: response.userHandle ? toBase64Url(response.userHandle) : null
    }
  };
}

export async function createPasskey(options: PublicKeyCredentialCreationOptionsJSON): Promise<PasskeyAttestation> {
  const credential = await navigator.credentials.create({ publicKey: creationOptions(options) });
  if (!(credential instanceof PublicKeyCredential) || !("attestationObject" in credential.response)) {
    throw new Error("浏览器未返回有效的 Passkey 创建结果");
  }
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    ...credentialFields(credential),
    response: {
      clientDataJSON: toBase64Url(response.clientDataJSON),
      attestationObject: toBase64Url(response.attestationObject),
      transports: response.getTransports?.() as AuthenticatorTransport[] | undefined
    }
  };
}

export function formatPasskeyError(error: unknown): string {
  if (error instanceof DOMException) {
    switch (error.name) {
      case "AbortError":
      case "NotAllowedError":
        return "浏览器未完成 Passkey 操作，请重试";
      case "InvalidStateError":
        return "这个 Passkey 已绑定";
      case "SecurityError":
        return "当前网址无法使用 Passkey，请检查站点域名与 HTTPS 配置";
    }
  }
  return formatError(error);
}
