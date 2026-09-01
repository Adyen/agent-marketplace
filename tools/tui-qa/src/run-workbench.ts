import process from "node:process";

import { loadConfig } from "./config.js";
import { installSignalAbort } from "./runner.js";
import { sanitizeMultiline } from "./sanitize.js";
import { runWorkbench } from "./workbench.js";

const main = async (): Promise<number> => {
  const config = loadConfig();
  if (process.env.AM_TUI_QA_CONTAINED !== "1") {
    process.stderr.write(
      "WARNING: native TUI QA executes trusted working-tree code with functional isolation only; " +
      "it is not a hostile-code security boundary. Use the contained runner for untrusted code.\n"
    );
    if (!config.nativeWarningAcknowledged) process.stderr.write("Set AM_TUI_QA_NATIVE_WARNING_ACK=1 to acknowledge this warning.\n");
  }

  const controller = new AbortController();
  const removeSignalHandlers = installSignalAbort(controller);
  try {
    const results = await runWorkbench(config, { signal: controller.signal });
    process.stdout.write(`TUI QA ${results.finalStatus}: ${config.resultsPath}\nTUI QA report: ${config.reportPath}\n`);
    return config.runClass === "baseline" ? 0 : results.exitCode;
  } finally {
    removeSignalHandlers();
  }
};

main()
  .then((exitCode) => {
    process.exitCode = exitCode;
  })
  .catch((error) => {
    process.stderr.write(`TUI QA invalid: ${sanitizeMultiline(error instanceof Error ? error.message : String(error))}\n`);
    process.exitCode = 3;
  });
