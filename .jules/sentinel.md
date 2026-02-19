## 2025-05-19 - Symlink Traversal in File Writes
**Vulnerability:** The `ResolveWritePath` function validated the symlink's location within the workspace but failed to resolve the symlink's *target* before authorizing the write. This allowed an attacker to create a symlink inside the workspace pointing to a sensitive file outside (e.g., `/etc/passwd`) and overwrite it.
**Learning:** Checking that a path string is lexically within a directory is insufficient when the filesystem supports symlinks. The operating system resolves symlinks at access time, bypassing string-based checks.
**Prevention:** Always resolve the final path using `filepath.EvalSymlinks` (or `os.Readlink`) for existing files before performing security checks or file operations. Treat the filesystem as untrusted input.

## 2025-05-20 - Unprotected Read Access to Control Files
**Vulnerability:** The `ResolveReadPath` function allowed read access to sensitive control files (e.g., `master.key`, `secrets.enc`) because the `isProtectedControlPath` check was only applied during write operations.
**Learning:** Security controls often focus on preventing modification (write), but read access can be just as critical for confidentiality. Access control checks must be consistent across all operations (read/write/execute).
**Prevention:** Apply the same policy validation logic (e.g., protected path checks) to both read and write operations unless there is a specific reason to differ.
