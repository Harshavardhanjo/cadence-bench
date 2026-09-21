/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export. The measurement runs in the visitor's browser through
  // WebAssembly, so there is no server: the whole UI is files. For this
  // benchmark that is not only convenient but required, since a hosted backend
  // would measure a machine the reader cannot see.
  output: "export",
  reactStrictMode: true,
};

export default nextConfig;
