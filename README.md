# Shuttle
**Version 1.12**

This project has been migrated from [ELN_file_watcher](https://github.com/ComPlat/ELN_file_watcher).

Shuttle is a utility that facilitates seamless file transfer from a source device to a target location, specifically for Chemotion. It automates the movement of files from a specified source directory to a destination server using WebDAV or SFTP.

---

## **Setup Systems**

1. **Using ShuttleBuilder**
   - Use the [ShuttleBuilder](https://github.com/ComPlat/shuttlebuilder) to build an executable instance of Shuttle.
   - Detailed setup instructions are available in the ShuttleBuilder repository.

2. **Manual Compilation**
   - If you prefer not to use ShuttleBuilder, compile Shuttle using Go version `1.23`.
   - Copy the executable to your source device and follow the **Usage** section for configuration.



---

## **Important Notes**

1. **Windows XP Compatibility**
   - To run this tool on Windows XP, it must be compiled with Go version `1.10.8`.
   - When using the SFTP protocol on Windows XP, save a portable version of [WinSCP](https://winscp.net/download/WinSCP-5.21.5-Portable.zip) in the same folder as `shuttle.exe`.

---

## **Usage**

> **Note:** If you used the [ShuttleBuilder](https://github.com/ComPlat/shuttlebuilder), CLI arguments are preconfigured and do not need to be manually added.

To run the utility, execute the following command:

```shell
shuttle -src < source_directory_path > -duration < time_in_seconds > -dst < destination_server > -user < username > -pass < password > -type < file|folder|zip|tar|flat_tar > -transfer < webdav|sftp > -commonRegex < Regexp >
```

### Parameters:

- < source_directory_path >: Path to the source directory containing files to transfer.
- < time_in_seconds >: Time threshold (in seconds) for determining if a file is ready for transfer (unchanged for the specified duration).
- < destination_server >: URL of the WebDAV/SFTP server.
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

## Configuration Checklist

Before running Shuttle, configure the following:
- WebDAV or SFTP server settings.
- Source and destination directory paths.
- File modification duration threshold.
- User credentials for authentication.
- Transfer type and protocol.

## Contributing

We welcome contributions! If you encounter issues or have suggestions for improvement, please:

1) Open an issue.
2) Submit a pull request.


## License
This project is licensed under the MIT License.







  

