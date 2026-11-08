use signal_hook::{
    consts::{SIGHUP, SIGINT, SIGTERM},
    iterator::Signals,
};
use std::{
    io::{BufRead, BufReader},
    process::{Command, Stdio},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    thread::{self, JoinHandle},
    time::Duration,
};
use tauri::{
    webview::{Color, NewWindowResponse},
    LogicalPosition, Manager, Theme, TitleBarStyle, WebviewUrl, WebviewWindowBuilder,
};

struct Supervisor {
    quitting: Arc<AtomicBool>,
    thread: Mutex<Option<JoinHandle<()>>>,
}

fn main() {
    let quitting = Arc::new(AtomicBool::new(false));
    let supervisor = Supervisor {
        quitting: quitting.clone(),
        thread: Mutex::new(None),
    };

    tauri::Builder::default()
        .manage(supervisor)
        .setup(move |app| {
            let mut signals = Signals::new([SIGTERM, SIGINT, SIGHUP])?;
            let exit = app.handle().clone();
            thread::spawn(move || {
                if signals.forever().next().is_some() {
                    exit.exit(0);
                }
            });
            // The sidecar's URL is known only once it listens; provider sign-in pages open in
            // the default browser, not in a webview window.
            let window = WebviewWindowBuilder::new(
                app,
                "main",
                WebviewUrl::External("about:blank".parse().expect("valid URL")),
            )
            .title("Reef")
            .title_bar_style(TitleBarStyle::Overlay)
            .hidden_title(true)
            .traffic_light_position(LogicalPosition::new(18.0, 33.0))
            .theme(Some(Theme::Dark))
            .background_color(Color(19, 19, 19, 255))
            .inner_size(1200.0, 800.0)
            .min_inner_size(700.0, 500.0)
            .disable_drag_drop_handler()
            .on_new_window(|url, _| {
                if matches!(url.scheme(), "http" | "https" | "mailto") {
                    if let Err(err) = Command::new("open").arg(url.as_str()).spawn() {
                        eprintln!("open {url}: {err}");
                    }
                }
                NewWindowResponse::Deny
            })
            .on_navigation(|url| {
                if url.scheme() == "about"
                    || (url.host_str() == Some("127.0.0.1") && url.port() == Some(4317))
                {
                    return true;
                }
                if matches!(url.scheme(), "http" | "https" | "mailto") {
                    if let Err(err) = Command::new("open").arg(url.as_str()).spawn() {
                        eprintln!("open {url}: {err}");
                    }
                }
                false
            })
            .build()?;
            let binary = std::env::current_exe()?.with_file_name("atoll");
            let handle = thread::spawn(move || {
                let mut navigated = false;
                while !quitting.load(Ordering::SeqCst) {
                    match Command::new(&binary)
                        .args([
                            "serve",
                            "--addr",
                            "127.0.0.1:4317",
                            "--exit-on-stdin-close",
                        ])
                        .stdin(Stdio::piped())
                        .stdout(Stdio::piped())
                        .stderr(Stdio::piped())
                        .spawn()
                    {
                        Ok(mut child) => {
                            let _stdin = child.stdin.take().expect("piped stdin");
                            let stdout = child.stdout.take().expect("piped stdout");
                            let stderr = child.stderr.take().expect("piped stderr");
                            let (url_tx, url_rx) = std::sync::mpsc::channel();
                            let stdout_thread = thread::spawn(move || {
                                for line in BufReader::new(stdout).lines().map_while(Result::ok) {
                                    if let Some(url) = line.strip_prefix("listening on ") {
                                        let _ = url_tx.send(url.to_owned());
                                    }
                                }
                            });
                            let (port_error_tx, port_error_rx) = std::sync::mpsc::channel();
                            let stderr_thread = thread::spawn(move || {
                                for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                                    if line.contains("127.0.0.1:4317 is in use by another program") {
                                        let _ = port_error_tx.send(());
                                    } else {
                                        eprintln!("sidecar: {line}");
                                    }
                                }
                            });
                            loop {
                                if quitting.load(Ordering::SeqCst) {
                                    let _ = child.kill();
                                    let _ = child.wait();
                                    let _ = stdout_thread.join();
                                    let _ = stderr_thread.join();
                                    return;
                                }
                                if !navigated {
                                    if let Ok(url) = url_rx.try_recv() {
                                        match url.parse() {
                                            Ok(url) => match window.navigate(url) {
                                                Ok(()) => navigated = true,
                                                Err(err) => eprintln!("navigate: {err}"),
                                            },
                                            Err(err) => eprintln!("invalid sidecar URL: {err}"),
                                        }
                                    }
                                }
                                match child.try_wait() {
                                    Ok(Some(status)) => {
                                        eprintln!("sidecar exited: {status}");
                                        let _ = stdout_thread.join();
                                        let _ = stderr_thread.join();
                                        if port_error_rx.try_recv().is_ok() {
                                            show_port_error(&window);
                                            return;
                                        }
                                        break;
                                    }
                                    Err(err) => {
                                        eprintln!("wait for sidecar: {err}");
                                        let _ = child.kill();
                                        let _ = child.wait();
                                        let _ = stdout_thread.join();
                                        let _ = stderr_thread.join();
                                        break;
                                    }
                                    Ok(None) => thread::sleep(Duration::from_millis(100)),
                                }
                            }
                        }
                        Err(err) => eprintln!("start sidecar: {err}"),
                    }
                    for _ in 0..20 {
                        if quitting.load(Ordering::SeqCst) {
                            return;
                        }
                        thread::sleep(Duration::from_millis(100));
                    }
                }
            });
            *app.state::<Supervisor>().thread.lock().unwrap() = Some(handle);
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("build Reef")
        .run(|app, event| {
            if matches!(event, tauri::RunEvent::Exit) {
                let supervisor = app.state::<Supervisor>();
                supervisor.quitting.store(true, Ordering::SeqCst);
                let thread = supervisor.thread.lock().unwrap().take();
                if let Some(thread) = thread {
                    let _ = thread.join();
                }
            }
        });
}

fn show_port_error(window: &tauri::WebviewWindow) {
    let script = r#"document.documentElement.innerHTML = '<head><title>Reef</title></head><body style="background:#131313;color:#fff;font:16px system-ui;padding:48px"><h1>Reef can’t start</h1><p>Port 4317 is in use by another program.</p></body>';"#;
    if let Err(err) = window.eval(script) {
        eprintln!("show port error: {err}");
    }
}
