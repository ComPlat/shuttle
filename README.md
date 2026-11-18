# Shuttle
**Version 1.14**

This project has been migrated from [ELN_file_watcher](https://github.com/ComPlat/ELN_file_watcher).

Shuttle is a utility that facilitates seamless file transfer from a source device to a target location, specifically for Chemotion. It automates the movement of files from a specified source directory to a destination server using WebDAV or SFTP, or hands them to a ChemAssistentManager job (HTTP upload).

---

## **Setup Systems**

1. **Using ShuttleBuilder**
   - Use the [ShuttleBuilder](https://github.com/ComPlat/shuttlebuilder) to build an executable instance of Shuttle.
   - Detailed setup instructions are available in the ShuttleBuilder repository.

2. **Manual Compilation**
   - If you prefer not to use ShuttleBuilder, compile Shuttle with Go `1.24` or newer (`go build`), for example `GOOS=windows GOARCH=amd64 go build` for Windows 10/11.
   - For Windows XP use `bash dev/build_xp.sh [repo_dir] [out_dir]` (needs Docker): it builds `shuttle.exe` with Go 1.10.8 exactly like ShuttleBuilder does.
   - Copy the executable to your source device and follow the **Usage** section for configuration.
   - `shuttle -version` prints the version.



---

## **Important Notes**

1. **Windows XP Compatibility**
   - To run this tool on Windows XP, it must be compiled with Go version `1.10.8`.
   - When using the SFTP protocol on Windows XP, save a portable version of [WinSCP](https://winscp.net/download/WinSCP-5.21.5-Portable.zip) in the same folder as `shuttle.exe` (`WinSCP.com` must be there). Shuttle writes a short temporary script into its shuttle folder, runs `WinSCP.com` on it once per uploaded file and deletes it afterwards. The password is never written to the log. At startup a connection check is run, so a missing `WinSCP.com` or a rejected host key shows up immediately in the log.
   - **Host key:** pass the server's SSH host key fingerprint with `-hostkey`, e.g. `-hostkey "ssh-ed25519 255 SHA256:AbCd..."`. Without `-hostkey`, WinSCP must already know the key: connect once with WinSCP on that machine and accept the key, otherwise every upload fails (Shuttle logs a warning at startup).
   - **Getting the fingerprint:** in WinSCP, connect to the server and open *Session → Server/Protocol Information*; copy the host key fingerprint shown there. On Linux, `ssh-keyscan -p <port> <host> 2>/dev/null | ssh-keygen -lf -` prints it (use the `SHA256:...` value).

---

## **Usage**

> **Note:** If you used the [ShuttleBuilder](https://github.com/ComPlat/shuttlebuilder), CLI arguments are preconfigured and do not need to be manually added.

To run the utility, execute the following command:

```shell
shuttle -src < source_directory_path > -duration < time_in_seconds > -dst < destination_server > -user < username > -pass < password > -type < file|folder|zip|tar|flat_tar > -transfer < webdav|sftp|chemassistent > -commonRegex < Regexp > [-crt < cert.pem >] [-hostkey < fingerprint >] [-insecure 1]
```

### Parameters:

- < source_directory_path >: Path to the source directory containing files to transfer.
- < time_in_seconds >: Time threshold (in seconds) for determining if a file is ready for transfer (unchanged for the specified duration).
- < destination_server >: the destination.
  - WebDAV: a full URL, `https://server/path/` (or `http://`).
  - SFTP: `host:port/path`, `ssh://host:port/path` or `sftp://host:port/path`; the port defaults to 22.
- < username >: Username for authentication.
- < password >: Password for authentication.
- Transfer Type (-type):
  - file: Transfers individual files, ignoring the source directory structure.
  - folder: Transfers entire folders only when all files in them are ready.
  - zip: Transfers folders as ZIP archives when all files in the folder are ready.
  - tar: Transfers folders as .tar.gz archives when all files in the folder are ready.
  - flat_tar: Transfers folders as .tar.gz archives, grouping files by a common regex.

If using flat_tar, provide a regular expression (< Regexp >) to group related files into datasets for archiving. Files with matching regex groups (or global matches if no groups are used) are archived together.

Transfer Protocol (-transfer):
- webdav: Use WebDAV protocol.
- sftp: Use SFTP protocol.
- chemassistent: Upload to a [ChemAssistentManager](https://github.com/ComPlat) job (see below).

### Transfer type `chemassistent` (ChemAssistentManager)

Shuttle sends each file (or each folder) to a ChemAssistentManager job by HTTP POST; every upload starts **one run** of the job's AI agent, which gets the files as input.

1. In ChemAssistentManager create a job with the trigger **"HTTP file upload (API key)"** and create an **API key** for it (it is shown only once).
2. Start Shuttle with `-transfer chemassistent -dst https://<server> -pass <API key>` (`-user` is ignored). `/api/jobs/trigger` is appended to `-dst` when it has no path; a full trigger URL works too.

- `-type file` / `flat_tar`: one run per file. `-type zip` / `tar`: one run per archive. `-type folder`: **one run per folder** with all its files; folder paths are flattened into the file names (`RunA/B/x.csv` → `RunA__B__x.csv`), because the server keeps only file names.
- Each run gets the message `Shuttle <name>: <path>`, so the AI knows where the data came from.
- A file is removed locally only when the server confirms it (`202`). A revoked key, a disabled job or a too large upload keeps the files and logs the server's answer; they are sent once the problem is fixed (new key → rebuild/restart Shuttle).
- `-maxUpload <MB>` (default 100, like the server's `JOB_MAX_UPLOAD_MB`): larger uploads are not attempted and stay local.
- TLS, `-crt` and `-insecure` work as for WebDAV. Windows XP builds speak at most TLS 1.2.
- `-preconvert` is not supported with this transfer type.
- If the connection breaks after the server stored a file but before Shuttle got the answer, the retry starts a second run.
- The API key is never written to the log.

Further options: `-name` (instance name, used for the working folder `~/shuttle/shuttle_<name>`), `-crt`, `-hostkey`, `-insecure` (see *Security*), and `-preconvert 1` with `-dstConverter`, `-userConverter`, `-passConverter` and `-public` to convert files with `ChemConverterApp` and send the converted files to a second destination.

### Running and stopping

- At startup Shuttle checks its setup and stops with a clear message if something can never work: invalid arguments, an unreadable `~/shuttle/scripts` folder, or a missing `ChemConverterApp` when `-preconvert` is set.
- Network problems are never fatal: if the server cannot be reached (also at startup, e.g. when the PC boots before the network is up), the error is logged and the upload is retried with increasing waits.
- A failing script or converter at runtime is logged; the conversion is skipped, the raw data is still sent.
- Connections are kept open between uploads and re-established after an error. WebDAV uploads are streamed, so large files do not need to fit into memory.
- `Ctrl+C` or `SIGTERM` stops Shuttle cleanly. Files not yet sent stay in the working folder and are sent after the next start.

### Preprocess Scripts

Shuttle supports preprocess scripts, which allow you to execute custom scripts before transferring files.

- Windows: only regular files with the extension `.exe` (any case) are run; `.bat`, `.cmd` and `.sh` are not.
- Linux: every regular file with an execute bit is run, whatever its extension. A symlink to such a file is run too.
- Directories and hidden files (names starting with `.`) are never run.
- The converter `ChemConverterApp` (`ChemConverterApp.exe` on Windows) must be next to the `shuttle` executable.

#### Preprocess Script Requirements:
The script files must be located in the directory:
```plaintext
~/shuttle/scripts
```

If the shuttle has already been executed, the folder has already been created.

Such a script can manipulate the file before it is sent. The script is called for each file that will be sent. The script gets one argument with the absolute path to the file. The script should either overwrite the file or write a new copy of the file in the same folder

### Which files are sent (sent register)

Shuttle remembers every file it has handed over for upload in `~/shuttle/shuttle_<name>/sent_register.json` (path relative to `-src`, modification time and size).

- A file (`-type file` / `flat_tar`) is sent when it is new or has changed since it was last sent, has not been modified for `-duration` seconds, and is not locked by another program.
- A folder (`-type folder` / `zip` / `tar`) is sent when at least one of its files is new or changed, and all of its files have been unchanged for `-duration` seconds and are unlocked. A changed folder is always sent again **completely**.
- **First start, or an empty or missing register:** every file already in `-src` that is older than `-duration` is sent once. When updating from Shuttle 1.12 or older, existing data in `-src` is uploaded again.
- To send everything again, stop Shuttle and delete `sent_register.json`. A damaged register is moved aside as `sent_register.json.corrupt-<time>`, and Shuttle starts with an empty register.
- If `-src` is temporarily unreachable (e.g. a network drive), the register is kept unchanged.

A local file is only deleted from Shuttle's working folders after its upload has been confirmed; failed uploads are retried with increasing waits (up to `-duration`).

### Security: certificates and host keys

**WebDAV (TLS).** Shuttle verifies the server certificate, using the system root certificates plus the optional `-crt` file (PEM). A missing or unreadable `-crt` file is logged as a warning. A server with a **self-signed certificate** needs `-crt` pointing to that certificate (or its CA); otherwise uploads fail until it is set. Files wait in Shuttle's working folders and are sent once the connection works. **Windows XP:** the XP build cannot read the Windows certificate store, so it verifies only when `-crt` is given; without `-crt` it does not verify and logs a warning at start.

**SFTP (SSH host key).**
- `-hostkey "<fingerprint>"` pins the server's key. Accepted forms: `SHA256:...`, WinSCP style `ssh-ed25519 255 SHA256:...`, or MD5 `aa:bb:...`. Any of the server's key types can be pinned.
- Without `-hostkey`, the key seen on the first connection is trusted and stored in `~/shuttle/shuttle_<name>/known_hosts` (OpenSSH format). If the server later presents a different key, Shuttle refuses to connect and logs which key it saw. If the change is intended (e.g. a reinstalled server), delete the server's line from `known_hosts` or set `-hostkey`.
- On Windows XP, `-hostkey` is passed to WinSCP; without it, WinSCP must already know the key (see *Windows XP Compatibility*).

**`-insecure 1`** turns off both checks (TLS certificates and SSH host keys) and logs a warning. Use it only for testing.

## Configuration Checklist

Before running Shuttle, configure the following:
- WebDAV or SFTP server settings.
- Source and destination directory paths.
- File modification duration threshold.
- User credentials for authentication.
- Transfer type and protocol.

## Changelog

### 1.14
- New transfer type **`chemassistent`**: upload files to a ChemAssistentManager job (HTTP POST with API key); one upload = one AI run, folders as one run. New flag `-maxUpload`.

### 1.13
- **No lost or duplicated uploads:** a local file is deleted only after a confirmed upload (WebDAV errors used to count as success); every zip/tar is uploaded once (was twice); files are handed over to the uploader atomically; flat_tar no longer deletes sources before a successful copy.
- **Sent register** (`sent_register.json`) replaces the timing window: files can no longer be skipped silently. **The first start after the update sends every stable file in `-src` once.**
- **Windows:** correct `/` entry names in zip/tar, working `-preconvert`, instance names with special characters, clear script rules.
- **Windows XP (SFTP):** WinSCP is driven by a script file: paths with spaces, special characters in passwords and nested folders work; failures are logged (password removed). *Not yet tested on a real XP machine.*
- **Security:** WebDAV certificates are verified (`-crt`, `-insecure`); SSH host keys are pinned (`-hostkey`) or trusted on first use (`known_hosts`); updated SSH library (x/crypto v0.48.0).
- **Robustness:** network errors at startup are retried instead of stopping Shuttle; no crash on runtime errors; WebDAV uploads are no longer cut off after 10 seconds; streamed uploads; connection reuse; `host:port/path` works as SFTP destination; `-version`.
- Tests and CI for Linux, Windows 10/11 and Windows XP builds.

## Contributing

We welcome contributions! If you encounter issues or have suggestions for improvement, please:

1) Open an issue.
2) Submit a pull request.


## License
This project is licensed under the MIT License.







  

