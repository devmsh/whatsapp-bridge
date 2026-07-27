import Foundation
import LocalAuthentication

/// Touch ID second factor for hidden chats, for the macOS app.
///
/// The web UI's own second factor is a WebAuthn assertion, which cannot work
/// here: Apple requires an app hosting a WKWebView to declare the relying
/// party as an associated domain before passkeys are usable, and that needs a
/// paid Team ID plus an apple-app-site-association file over HTTPS — neither
/// of which a loopback bridge can have. A Secure Enclave key is out too:
/// persisting one needs a keychain access group, also Team-ID-gated
/// (SecItemAdd returns -34018 under ad-hoc signing, on both keychain
/// backends — verified, not assumed).
///
/// LocalAuthentication itself works fine ad-hoc signed, so that is what we
/// use. The shell proves it is the local shell with an owner-only key file;
/// see internal/api/handler_hidden_native.go for why that is an honest
/// boundary rather than a downgrade.
enum NativeUnlock {
    enum Failure: Error {
        case biometricsUnavailable(String)
        case cancelled
        case authFailed(String)
        case keyUnavailable(String)
        case bridge(String)
    }

    /// Is Touch ID usable at all? The UI asks before offering the button.
    static func available() -> Bool {
        var err: NSError?
        return LAContext().canEvaluatePolicy(
            .deviceOwnerAuthenticationWithBiometrics, error: &err)
    }

    /// Prompts for Touch ID and returns an unlock token.
    ///
    /// Touch ID is the primary credential, as on macOS itself. `pinPassedToken`
    /// is only supplied on the fallback path, when Touch ID was unavailable and
    /// the user entered their PIN instead; passing it skips the biometric
    /// prompt entirely.
    static func unlock(
        pinPassedToken: String? = nil,
        completion: @escaping (Result<String, Failure>) -> Void
    ) {
        // PIN fallback: the user already proved themselves, don't ask again.
        if let pinToken = pinPassedToken, !pinToken.isEmpty {
            readKey { keyResult in
                switch keyResult {
                case .failure(let f): completion(.failure(f))
                case .success(let key):
                    exchange(method: "pin", pinPassedToken: pinToken, key: key,
                             completion: completion)
                }
            }
            return
        }

        let ctx = LAContext()
        ctx.localizedFallbackTitle = "" // no "Enter Password" — PIN was step one

        var policyErr: NSError?
        guard ctx.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &policyErr)
        else {
            completion(
                .failure(.biometricsUnavailable(
                    policyErr?.localizedDescription ?? "Touch ID is not available")))
            return
        }

        ctx.evaluatePolicy(
            .deviceOwnerAuthenticationWithBiometrics,
            localizedReason: "unlock your hidden chats"
        ) { ok, err in
            guard ok else {
                if let laErr = err as? LAError, laErr.code == .userCancel {
                    completion(.failure(.cancelled))
                } else {
                    completion(
                        .failure(.authFailed(err?.localizedDescription ?? "Touch ID failed")))
                }
                return
            }
            // Only AFTER a real biometric success do we touch the key.
            readKey { keyResult in
                switch keyResult {
                case .failure(let f):
                    completion(.failure(f))
                case .success(let key):
                    exchange(method: "biometric", pinPassedToken: "", key: key,
                             completion: completion)
                }
            }
        }
    }

    /// Asks the bridge where its key file is, then reads it. The path is not
    /// hardcoded so a relocated BRIDGE_HOME keeps working.
    private static func readKey(_ completion: @escaping (Result<String, Failure>) -> Void) {
        var req = URLRequest(url: Config.api("hidden/native/key-path"))
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { data, _, err in
            if let err {
                completion(.failure(.keyUnavailable(err.localizedDescription)))
                return
            }
            guard let data,
                let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                let path = obj["path"] as? String
            else {
                completion(.failure(.keyUnavailable("bridge did not return a key path")))
                return
            }
            do {
                let key = try String(contentsOfFile: path, encoding: .utf8)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                completion(.success(key))
            } catch {
                completion(.failure(.keyUnavailable(
                    "could not read \(path): \(error.localizedDescription)")))
            }
        }.resume()
    }

    private static func exchange(
        method: String,
        pinPassedToken: String,
        key: String,
        completion: @escaping (Result<String, Failure>) -> Void
    ) {
        var req = URLRequest(url: Config.api("hidden/unlock/native"))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 15
        req.httpBody = try? JSONSerialization.data(withJSONObject: [
            "method": method,
            "pin_passed_token": pinPassedToken,
            "key": key,
        ])

        URLSession.shared.dataTask(with: req) { data, resp, err in
            if let err {
                completion(.failure(.bridge(err.localizedDescription)))
                return
            }
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            guard let data,
                let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            else {
                completion(.failure(.bridge("unreadable response (HTTP \(code))")))
                return
            }
            if let token = obj["unlock_token"] as? String, !token.isEmpty {
                completion(.success(token))
            } else {
                let msg = (obj["error"] as? String) ?? "unlock refused (HTTP \(code))"
                completion(.failure(.bridge(msg)))
            }
        }.resume()
    }
}
