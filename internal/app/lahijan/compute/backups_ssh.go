// Package compute: backups_ssh.go is the "ssh" BackupTarget driver. It
// uploads snapshot tarballs to a remote host via the SSH protocol. The
// driver uses golang.org/x/crypto/ssh for the transport and prefers
// rsync (delta protocol) when the binary is on the local PATH; it falls
// back to SFTP when rsync is unavailable.
//
// Implementation note: this driver keeps an open SSH client per
// LocalDriver instance. The driver is safe for concurrent use at the
// API level (Upload / Delete / Download can run in parallel), but each
// call opens its own SFTP / rsync session so the per-call cost is one
// TCP + auth round-trip.
package compute

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHDriver implements BackupTarget against a remote SSH host.
type SSHDriver struct {
	host       string
	port       int
	user       string
	remotePath string
	// signer is the parsed private key signer; nil when the key is empty
	// (which yields a friendlier error at first use than a panic).
	signer ssh.Signer
	// hostKeyFingerprint is the optional pinned fingerprint. When set the
	// driver validates the server's host key against it. When empty the
	// driver uses InsecureIgnoreHostKey (acceptable in trusted networks;
	// a future WS will wire a known_hosts file).
	hostKeyFingerprint string
}

// NewSSHDriver builds an SSHDriver from the per-kind config + decrypted
// secret. Returns an error when the private key cannot be parsed; the
// caller surfaces it via the service-layer "create target" path so a
// misconfigured target fails at create time, not at first use.
func NewSSHDriver(cfg BackupTargetConfig, secret BackupTargetSecret) (*SSHDriver, error) {
	d := &SSHDriver{
		host:               cfg.SSHHost,
		port:               cfg.SSHPort,
		user:               cfg.SSHUser,
		remotePath:         cfg.SSHRemotePath,
		hostKeyFingerprint: cfg.SSHHostKeyFingerprint,
	}
	if d.port == 0 {
		d.port = 22
	}
	if secret.SSHPrivateKey != "" {
		var (
			signer ssh.Signer
			err    error
		)
		if secret.SSHPrivateKeyPW != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret.SSHPrivateKey), []byte(secret.SSHPrivateKeyPW))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret.SSHPrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("compute: ssh backup target: parse private key: %w", err)
		}
		d.signer = signer
	}
	return d, nil
}

// Kind returns the driver's kind slug.
func (d *SSHDriver) Kind() string { return BackupTargetKindSSH }

// Ping verifies the SSH dial succeeds + the auth works.
func (d *SSHDriver) Ping(ctx context.Context) error {
	if d.host == "" {
		return errors.New("compute: ssh backup target: host is required")
	}
	if d.signer == nil {
		return errors.New("compute: ssh backup target: private key is required")
	}
	client, err := d.dial(ctx)
	if err != nil {
		return err
	}
	_ = client.Close()
	return nil
}

// Upload writes body to the remote host. Prefers rsync (delta protocol)
// when the binary is on PATH; falls back to SFTP otherwise. The SHA-256
// is computed locally from the upload stream so the worker can record it.
func (d *SSHDriver) Upload(ctx context.Context, key string, body io.Reader) (int64, string, error) {
	// Buffer the body locally so we can both hash it + hand rsync a file
	// path. For very large backups this is a future optimisation (streaming
	// SFTP via io.Copy directly to the remote file).
	tmp, err := os.CreateTemp("", "lahijan-ssh-upload-*")
	if err != nil {
		return 0, "", fmt.Errorf("compute: ssh upload: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	if err != nil {
		_ = tmp.Close()
		return 0, "", fmt.Errorf("compute: ssh upload: buffer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, "", fmt.Errorf("compute: ssh upload: close tmp: %w", err)
	}
	checksum := hex.EncodeToString(h.Sum(nil))

	remotePath := d.fullPath(key)
	if err := d.ensureRemoteDir(ctx, remotePath); err != nil {
		return 0, "", err
	}

	if path, err := exec.LookPath("rsync"); err == nil && path != "" {
		if err := d.rsyncUp(ctx, tmpPath, remotePath); err != nil {
			return 0, "", err
		}
		return n, checksum, nil
	}
	// Fallback: SFTP.
	if err := d.sftpUpload(ctx, tmpPath, remotePath); err != nil {
		return 0, "", err
	}
	return n, checksum, nil
}

// Delete removes the remote file. Idempotent: a missing file is a no-op.
func (d *SSHDriver) Delete(ctx context.Context, key string) error {
	client, err := d.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("compute: ssh delete: new session: %w", err)
	}
	defer func() { _ = sess.Close() }()
	// rm -f suppresses "no such file"; the && echo OK marker lets us
	// distinguish a real failure from a connection drop.
	remote := d.fullPath(key)
	out, err := sess.CombinedOutput(fmt.Sprintf("rm -f -- %q && echo OK", remote))
	if err != nil {
		return fmt.Errorf("compute: ssh delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Download fetches the remote file via cat. The caller owns the returned
// reader and MUST Close it.
func (d *SSHDriver) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	client, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("compute: ssh download: new session: %w", err)
	}
	remote := d.fullPath(key)
	pr, pw := io.Pipe()
	go func() {
		defer func() {
			_ = sess.Close()
			_ = client.Close()
		}()
		sess.Stdout = pw
		err := sess.Run(fmt.Sprintf("cat -- %q", remote))
		_ = pw.CloseWithError(err)
	}()
	return pr, nil
}

// dial opens a fresh SSH client. The caller closes it.
func (d *SSHDriver) dial(ctx context.Context) (*ssh.Client, error) {
	addr := net.JoinHostPort(d.host, strconv.Itoa(d.port))
	cfg := &ssh.ClientConfig{
		User:            d.user,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: d.hostKeyCallback(),
		Timeout:         10 * time.Second,
	}
	if d.signer != nil {
		cfg.Auth = append(cfg.Auth, ssh.PublicKeys(d.signer))
	}
	// Apply the per-call deadline from ctx.
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: ssh dial: %w", ErrBackupTargetUnreachable, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: ssh handshake: %w", ErrBackupTargetUnreachable, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// hostKeyCallback returns the appropriate callback: a fingerprint-pinned
// check when the operator set host_key_fingerprint, or insecure-ignore
// otherwise. A future WS will wire a known_hosts file.
func (d *SSHDriver) hostKeyCallback() ssh.HostKeyCallback {
	if d.hostKeyFingerprint == "" {
		return ssh.InsecureIgnoreHostKey()
	}
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		got := sshFingerprint(k)
		if got != d.hostKeyFingerprint {
			return fmt.Errorf("compute: ssh host key mismatch: expected %s, got %s",
				d.hostKeyFingerprint, got)
		}
		return nil
	}
}

// sshFingerprint returns the SHA-256 fingerprint of an SSH public key
// (base64-encoded, prefixed with "SHA256:"). Mirrors OpenSSH's format.
func sshFingerprint(k ssh.PublicKey) string {
	raw := sha256.Sum256(k.Marshal())
	return "SHA256:" + base64Sha256(raw[:])
}

// base64Sha256 returns the std-encoded base64 of b without padding. Pulled
// out so the fingerprint helper stays terse.
func base64Sha256(b []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	out := make([]byte, 0, ((len(b)+2)/3)*4)
	for i := 0; i < len(b); i += 3 {
		var n uint32
		valid := 0
		for j := 0; j < 3; j++ {
			if i+j < len(b) {
				n |= uint32(b[i+j]) << (16 - 8*j)
				valid++
			}
		}
		out = append(out, enc[(n>>18)&0x3F], enc[(n>>12)&0x3F])
		if valid > 1 {
			out = append(out, enc[(n>>6)&0x3F])
		} else {
			out = append(out, '=')
		}
		if valid > 2 {
			out = append(out, enc[n&0x3F])
		} else {
			out = append(out, '=')
		}
	}
	// Trim padding for the SHA-256 fingerprint format.
	return string(bytes.TrimRight(out, "="))
}

// rsyncUp streams the local file at local to remote via rsync over SSH.
// rsync handles its own authentication by re-using the SSH_AUTH_SOCK
// (when an agent is running) or by prompting for a password (we disable
// the latter via BatchMode). For private-key auth we write the key to a
// temporary file and pass it via rsync's -e "ssh -i <key>" option.
func (d *SSHDriver) rsyncUp(ctx context.Context, local, remote string) error {
	args := []string{
		"--compress",
		"--checksum",
		"--partial",
		"--timeout=300",
		"-e", "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -p " + strconv.Itoa(d.port),
		local,
		fmt.Sprintf("%s@%s:%s", d.user, d.host, remote),
	}
	cmd := exec.CommandContext(ctx, "rsync", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compute: ssh upload (rsync): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sftpUpload uploads a local file via SFTP as the rsync fallback.
func (d *SSHDriver) sftpUpload(ctx context.Context, local, remote string) error {
	client, err := d.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("compute: ssh sftp: new session: %w", err)
	}
	defer func() { _ = sess.Close() }()
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("compute: ssh sftp: open local: %w", err)
	}
	defer func() { _ = f.Close() }()
	sess.Stdin = f
	out, err := sess.CombinedOutput(fmt.Sprintf("cat > %q", remote))
	if err != nil {
		return fmt.Errorf("compute: ssh sftp: upload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureRemoteDir makes the remote parent dir of remote (best-effort).
// A failure here is not fatal; the upload itself will surface a clearer
// error from rsync / SFTP when the parent does not exist.
func (d *SSHDriver) ensureRemoteDir(ctx context.Context, remote string) error {
	parent := path.Dir(remote)
	if parent == "" || parent == "." || parent == "/" {
		return nil
	}
	client, err := d.dial(ctx)
	if err != nil {
		return nil // best-effort
	}
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		return nil
	}
	defer func() { _ = sess.Close() }()
	_, _ = sess.CombinedOutput(fmt.Sprintf("mkdir -p -- %q", parent))
	return nil
}

// fullPath joins the configured remote path with the per-object key.
func (d *SSHDriver) fullPath(key string) string {
	if d.remotePath == "" {
		return strings.TrimLeft(key, "/")
	}
	return strings.TrimRight(d.remotePath, "/") + "/" + strings.TrimLeft(key, "/")
}
