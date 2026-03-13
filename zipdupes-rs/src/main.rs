use clap::Parser;
use std::collections::HashMap;
use std::io::{self, Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::time::Instant;
use walkdir::WalkDir;

#[derive(Parser)]
#[command(about = "Find duplicate files across ZIP archives")]
struct Args {
    /// List files in each ZIP
    #[arg(short = 'l')]
    list_files: bool,

    /// With -l, list only duplicate files
    #[arg(short = 'o')]
    only_dups: bool,

    /// Show only ZIPs that are fully duplicated (can be deleted)
    #[arg(short = 'd')]
    show_dupes: bool,

    /// Directories to scan
    #[arg(required = true)]
    dirs: Vec<PathBuf>,
}

struct FileInfo {
    name: String,
    size: u64,
}

struct ZipInfo {
    _path: String,
    files: Vec<FileInfo>,
    total_size: u64,
    total_files: usize,
}

fn main() {
    let args = Args::parse();

    // Collect all ZIP files
    let mut zip_files: Vec<PathBuf> = Vec::new();
    for dir in &args.dirs {
        for entry in WalkDir::new(dir).into_iter().filter_map(|e| e.ok()) {
            if !entry.file_type().is_dir() {
                if let Some(ext) = entry.path().extension() {
                    if ext.eq_ignore_ascii_case("zip") {
                        zip_files.push(entry.into_path());
                    }
                }
            }
        }
    }

    if zip_files.is_empty() {
        println!("No ZIP files found");
        return;
    }

    println!("Found {} ZIP files", zip_files.len());

    // Read all ZIP contents
    let mut zip_infos: HashMap<String, ZipInfo> = HashMap::new();
    // file (name+size) -> list of ZIPs containing it
    let mut file_locations: HashMap<(String, u64), Vec<String>> = HashMap::new();

    let start_time = Instant::now();
    let mut last_update = Instant::now();
    let total = zip_files.len();

    for (i, path) in zip_files.iter().enumerate() {
        // Progress update every 200ms
        if last_update.elapsed().as_millis() > 200 || i == total - 1 {
            last_update = Instant::now();
            let elapsed = start_time.elapsed();
            let pct = (i + 1) as f64 / total as f64 * 100.0;

            let eta_str = if i > 0 {
                let rate = i as f64 / elapsed.as_secs_f64();
                let remaining = total - i - 1;
                if rate > 0.0 {
                    format_duration(std::time::Duration::from_secs_f64(remaining as f64 / rate))
                } else {
                    String::new()
                }
            } else {
                String::new()
            };

            let bar_width = 30;
            let filled = (bar_width as f64 * (i + 1) as f64 / total as f64) as usize;
            let bar: String =
                "=".repeat(filled) + &" ".repeat(bar_width - filled);

            print!(
                "\r[{}] {:5.1}% | {}/{} | ETA: {}   ",
                bar,
                pct,
                i + 1,
                total,
                eta_str
            );
            let _ = io::stdout().flush();
        }

        let path_str = path.to_string_lossy().to_string();
        match read_zip_info(path) {
            Ok(info) => {
                for f in &info.files {
                    file_locations
                        .entry((f.name.clone(), f.size))
                        .or_default()
                        .push(path_str.clone());
                }
                zip_infos.insert(path_str, info);
            }
            Err(e) => {
                eprintln!("\nError reading {}: {}", path_str, e);
            }
        }
    }

    let elapsed = start_time.elapsed();
    println!(
        "\r[{}] 100.0% | {}/{} | Done in {}   ",
        "=".repeat(30),
        total,
        total,
        format_duration(elapsed)
    );

    // Analyze duplicates
    let mut fully_duplicated: Vec<String> = Vec::new();
    let mut partially_duplicated: Vec<String> = Vec::new();

    for (path, info) in &zip_infos {
        if info.files.is_empty() {
            continue;
        }

        let mut duplicate_count = 0;
        for f in &info.files {
            let key = (f.name.clone(), f.size);
            if let Some(locations) = file_locations.get(&key) {
                if locations.len() > 1 {
                    duplicate_count += 1;
                }
            }
        }

        if duplicate_count == info.files.len() {
            fully_duplicated.push(path.clone());
        } else if duplicate_count > 0 {
            partially_duplicated.push(path.clone());
        }
    }

    fully_duplicated.sort();
    partially_duplicated.sort();

    // Output
    if args.list_files && !args.show_dupes {
        if args.only_dups {
            println!("\n=== Duplicate Files ===");
        } else {
            println!("\n=== ZIP Contents ===");
        }

        let mut paths: Vec<&String> = zip_infos.keys().collect();
        paths.sort();

        for path in paths {
            let info = &zip_infos[path];
            let mut zip_printed = false;

            for f in &info.files {
                let key = (f.name.clone(), f.size);
                let locations = file_locations.get(&key).unwrap();
                let is_dup = locations.len() > 1;

                if args.only_dups && !is_dup {
                    continue;
                }

                if !zip_printed {
                    println!(
                        "\n{} ({} files, {})",
                        path,
                        info.total_files,
                        format_size(info.total_size)
                    );
                    zip_printed = true;
                }

                if is_dup {
                    let mut others: Vec<String> = Vec::new();
                    let mut same_zip_count = 0;
                    for loc in locations {
                        if loc == path {
                            same_zip_count += 1;
                        } else {
                            others.push(basename(loc));
                        }
                    }

                    let dup_desc = if same_zip_count > 1 {
                        let mut desc = format!("{}x in same ZIP", same_zip_count);
                        if !others.is_empty() {
                            desc.push_str("; ");
                            desc.push_str(&others.join(", "));
                        }
                        desc
                    } else {
                        others.join(", ")
                    };

                    println!(
                        "  {} ({}) [LISTED in: {}; DUP in: {}]",
                        f.name,
                        format_size(f.size),
                        basename(path),
                        dup_desc
                    );
                } else {
                    println!("  {} ({})", f.name, format_size(f.size));
                }
            }
        }
    }

    if args.show_dupes {
        if fully_duplicated.is_empty() {
            println!("\nNo fully duplicated ZIPs found");
        } else {
            println!(
                "\n=== Fully Duplicated ZIPs (can be deleted): {} ===",
                fully_duplicated.len()
            );
            let mut total_size: u64 = 0;
            for path in &fully_duplicated {
                let info = &zip_infos[path];
                println!(
                    "{} ({} files, {})",
                    path,
                    info.total_files,
                    format_size(info.total_size)
                );
                total_size += info.total_size;

                if args.list_files {
                    for f in &info.files {
                        let key = (f.name.clone(), f.size);
                        let other_zips = &file_locations[&key];
                        let others: Vec<String> = other_zips
                            .iter()
                            .filter(|o| *o != path)
                            .map(|o| basename(o))
                            .collect();
                        println!(
                            "  {} ({}) -> also in: {}",
                            f.name,
                            format_size(f.size),
                            others.join(", ")
                        );
                    }
                }
            }
            println!(
                "\nTotal: {} ZIPs, {} can be freed",
                fully_duplicated.len(),
                format_size(total_size)
            );
        }
    } else {
        // Summary
        println!("\n=== Summary ===");
        println!("Total ZIPs: {}", zip_infos.len());
        println!(
            "Fully duplicated (can delete): {}",
            fully_duplicated.len()
        );
        println!("Partially duplicated: {}", partially_duplicated.len());
        println!(
            "Unique: {}",
            zip_infos.len() - fully_duplicated.len() - partially_duplicated.len()
        );

        if !fully_duplicated.is_empty() {
            println!("\nFully duplicated ZIPs:");
            let mut total_size: u64 = 0;
            for path in &fully_duplicated {
                let info = &zip_infos[path];
                total_size += info.total_size;
                println!("  {} ({})", path, format_size(info.total_size));
            }
            println!("Total size that can be freed: {}", format_size(total_size));
        }
    }
}

// Parse ZIP central directory directly — only 2 seeks per file, no per-entry I/O.
fn read_zip_info(path: &Path) -> Result<ZipInfo, Box<dyn std::error::Error>> {
    let mut file = std::fs::File::open(path)?;
    let file_len = file.metadata()?.len();

    if file_len < 22 {
        return Err("file too small for ZIP".into());
    }

    // Read last 65KB (max EOCD size with comment) to find End of Central Directory
    let search_len = std::cmp::min(file_len, 65557) as usize;
    file.seek(SeekFrom::End(-(search_len as i64)))?;
    let mut buf = vec![0u8; search_len];
    file.read_exact(&mut buf)?;

    // Find EOCD signature (0x06054b50) scanning backwards
    let eocd_pos = (0..=buf.len().saturating_sub(22))
        .rev()
        .find(|&i| buf[i..i + 4] == [0x50, 0x4b, 0x05, 0x06])
        .ok_or("could not find EOCD")?;

    let eocd = &buf[eocd_pos..];
    let mut cd_size = u32::from_le_bytes(eocd[12..16].try_into()?) as u64;
    let mut cd_offset = u32::from_le_bytes(eocd[16..20].try_into()?) as u64;

    // Check for ZIP64 EOCD locator
    if cd_offset == 0xFFFFFFFF || cd_size == 0xFFFFFFFF {
        if eocd_pos >= 20 {
            let loc = &buf[eocd_pos - 20..eocd_pos];
            if loc[0..4] == [0x50, 0x4b, 0x06, 0x07] {
                let eocd64_offset = u64::from_le_bytes(loc[8..16].try_into()?);
                file.seek(SeekFrom::Start(eocd64_offset))?;
                let mut eocd64_buf = [0u8; 56];
                file.read_exact(&mut eocd64_buf)?;
                if eocd64_buf[0..4] == [0x50, 0x4b, 0x06, 0x06] {
                    cd_size = u64::from_le_bytes(eocd64_buf[40..48].try_into()?);
                    cd_offset = u64::from_le_bytes(eocd64_buf[48..56].try_into()?);
                }
            }
        }
    }

    // Read entire central directory in one shot
    file.seek(SeekFrom::Start(cd_offset))?;
    let mut cd_buf = vec![0u8; cd_size as usize];
    file.read_exact(&mut cd_buf)?;

    let mut info = ZipInfo {
        _path: path.to_string_lossy().to_string(),
        files: Vec::new(),
        total_size: 0,
        total_files: 0,
    };

    // Parse central directory entries
    let mut pos = 0;
    while pos + 46 <= cd_buf.len() {
        // Check central directory entry signature
        if cd_buf[pos..pos + 4] != [0x50, 0x4b, 0x01, 0x02] {
            break;
        }

        let mut uncomp_size = u32::from_le_bytes(cd_buf[pos + 24..pos + 28].try_into()?) as u64;
        let name_len = u16::from_le_bytes(cd_buf[pos + 28..pos + 30].try_into()?) as usize;
        let extra_len = u16::from_le_bytes(cd_buf[pos + 30..pos + 32].try_into()?) as usize;
        let comment_len = u16::from_le_bytes(cd_buf[pos + 32..pos + 34].try_into()?) as usize;

        let name_start = pos + 46;
        let name_end = name_start + name_len;
        if name_end > cd_buf.len() {
            break;
        }

        let name = String::from_utf8_lossy(&cd_buf[name_start..name_end]).to_string();
        let is_dir = name.ends_with('/');

        // Handle ZIP64 extra field for large files
        if uncomp_size == 0xFFFFFFFF {
            let extra_start = name_end;
            let extra_end = extra_start + extra_len;
            if extra_end <= cd_buf.len() {
                let mut epos = extra_start;
                while epos + 4 <= extra_end {
                    let tag = u16::from_le_bytes(cd_buf[epos..epos + 2].try_into()?);
                    let sz = u16::from_le_bytes(cd_buf[epos + 2..epos + 4].try_into()?) as usize;
                    if tag == 0x0001 && epos + 4 + 8 <= extra_end {
                        // ZIP64 extended info: first 8 bytes are uncompressed size
                        uncomp_size =
                            u64::from_le_bytes(cd_buf[epos + 4..epos + 12].try_into()?);
                        break;
                    }
                    epos += 4 + sz;
                }
            }
        }

        if !is_dir {
            info.files.push(FileInfo {
                name,
                size: uncomp_size,
            });
            info.total_size += uncomp_size;
            info.total_files += 1;
        }

        pos = name_end + extra_len + comment_len;
    }

    Ok(info)
}

fn basename(path: &str) -> String {
    Path::new(path)
        .file_name()
        .map(|n| n.to_string_lossy().to_string())
        .unwrap_or_else(|| path.to_string())
}

fn format_size(bytes: u64) -> String {
    const KB: f64 = 1024.0;
    const MB: f64 = KB * 1024.0;
    const GB: f64 = MB * 1024.0;

    let b = bytes as f64;
    if b >= GB {
        format!("{:.2} GB", b / GB)
    } else if b >= MB {
        format!("{:.2} MB", b / MB)
    } else if b >= KB {
        format!("{:.2} KB", b / KB)
    } else {
        format!("{} B", bytes)
    }
}

fn format_duration(d: std::time::Duration) -> String {
    let total_secs = d.as_secs();
    if total_secs == 0 {
        return "<1s".to_string();
    }

    let h = total_secs / 3600;
    let m = (total_secs % 3600) / 60;
    let s = total_secs % 60;

    if h > 0 {
        format!("{}h{}m", h, m)
    } else if m > 0 {
        format!("{}m{}s", m, s)
    } else {
        format!("{}s", s)
    }
}
