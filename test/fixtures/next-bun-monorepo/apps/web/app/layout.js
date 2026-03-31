export const metadata = {
  title: "forge-metal bun fixture"
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}

