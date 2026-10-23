import com.licentivo.launch.Launch;
import java.nio.file.*;

public final class Main {
    public static void main(String[] args) {
        try { run(args); }
        catch (Launch.Failure failure) { System.err.println(failure.getMessage()); System.exit(1); }
        catch (Exception failure) { System.err.println("Program integration failed; inspect configuration and service availability. Credentials were not logged."); System.exit(1); }
    }
    private static void run(String[] args) throws Exception {
        if (args.length < 2 || args.length > 3) throw new IllegalArgumentException("usage: Main launch-config.json product.json [--logout|--release-device]");
        if (args.length == 3 && !args[2].equals("--logout") && !args[2].equals("--release-device")) throw new IllegalArgumentException("unknown action");
        var config = Launch.Config.load(Path.of(args[0]));
        var root = Path.of(System.getProperty("user.home"), ".cache", "LicentivoLaunchJava");
        try (var app = new Launch(config, Launch.readPublic(Path.of(args[1])), root)) {
            if (args.length == 3 && args[2].equals("--logout")) { app.logout(); System.out.println("Local session cleared."); return; }
            System.out.println("Memory-only login. Inject an OS-protected Store for remembered sessions across restarts.");
            try { app.restore(); }
            catch (Launch.Failure failure) {
                if (!failure.code.equals("NO_SESSION") && !failure.code.equals("SESSION_EXPIRED")) throw failure;
                var console = System.console(); if (console == null) throw new IllegalStateException("interactive console required for password entry");
                String email = console.readLine("Email: "); char[] password = console.readPassword("Password: ");
                if (email == null || password == null) throw new IllegalStateException("login canceled");
                app.login(email, password);
            }
            if (args.length == 3) {
                if (!args[2].equals("--release-device")) throw new IllegalArgumentException("unknown action");
                app.releaseDevice(); System.out.println("Device released."); return;
            }
            app.runFeature("export", () -> Files.writeString(Path.of("licensed-report.txt"), "Protected report\n"));
            System.out.println("Protected export completed.");
        }
    }
}
