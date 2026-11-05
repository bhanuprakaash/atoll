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
use tauri::Manager;

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
            let window = app.get_webview_window("main").expect("main window");
            let binary = std::env::current_exe()?.with_file_name("atoll");
            let handle = thread::spawn(move || {
                let mut navigated = false;
                while !quitting.load(Ordering::SeqCst) {
                    match Command::new(&binary)
                        .args(["serve", "--addr", "127.0.0.1:4317"])
                        .stdout(Stdio::piped())
                        .stderr(Stdio::inherit())
                        .spawn()
                    {
                        Ok(mut child) => {
                            let stdout = child.stdout.take().expect("piped stdout");
                            let (tx, rx) = std::sync::mpsc::channel();
                            thread::spawn(move || {
                                for line in BufReader::new(stdout).lines().map_while(Result::ok) {
                                    if let Some(url) = line.strip_prefix("listening on ") {
                                        let _ = tx.send(url.to_owned());
                                    }
                                }
                            });
                            loop {
                                if quitting.load(Ordering::SeqCst) {
                                    let _ = child.kill();
                                    let _ = child.wait();
                                    return;
                                }
                                if !navigated {
                                    if let Ok(url) = rx.try_recv() {
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
                                        break;
                                    }
                                    Err(err) => {
                                        eprintln!("wait for sidecar: {err}");
                                        let _ = child.kill();
                                        let _ = child.wait();
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
