import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "cadence-bench",
  description:
    "How well a loop holds a 20ms deadline, measured in your browser by the same Go harness as the command line tool, which refuses to report figures the clock cannot support.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-neutral-50 text-neutral-900 antialiased dark:bg-neutral-950 dark:text-neutral-100">
        {children}
      </body>
    </html>
  );
}
