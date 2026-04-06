export const metadata = {
  title: "forge-metal pnpm fixture"
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}

