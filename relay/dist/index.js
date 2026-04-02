"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const server_1 = require("@hocuspocus/server");
const extension_database_1 = require("@hocuspocus/extension-database");
const API_URL = process.env.HNB_API_URL || 'http://localhost:8080';
const PORT = parseInt(process.env.HNB_RELAY_PORT || '3001');
const server = new server_1.Hocuspocus({
    port: PORT,
    extensions: [
        new extension_database_1.Database({
            fetch: async ({ documentName }) => {
                const res = await fetch(`${API_URL}/internal/yjs/${documentName}`);
                if (!res.ok)
                    return null;
                const buf = await res.arrayBuffer();
                return new Uint8Array(buf);
            },
            store: async ({ documentName, state }) => {
                await fetch(`${API_URL}/internal/yjs/${documentName}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/octet-stream' },
                    body: state,
                });
            },
        }),
    ],
    async onAuthenticate({ token }) {
        const res = await fetch(`${API_URL}/internal/auth/validate`, {
            headers: { Authorization: `Bearer ${token}` },
        });
        if (!res.ok) {
            throw new Error('Unauthorized');
        }
    },
});
server.listen().then(() => {
    console.log(`Hocuspocus relay listening on port ${PORT}`);
});
