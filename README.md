# cloud-syncer

It uses**Google Drive API with a Service Account**. This method avoids interactive authentication and uses a service account key file for authentication, which is ideal for server environments.

---

### **Steps to Use a Service Account for Google Drive API**

1. **Enable the Google Drive API for Your Project**:
   - Go to the [Google Cloud Console](https://console.cloud.google.com/).
   - Enable the **Google Drive API** for your project.

2. **Create a Service Account**:
   - Navigate to **IAM & Admin > Service Accounts**.
   - Create a new service account and give it a descriptive name, e.g., `Drive API Service Account`.
   - Grant it the **Editor** role (or more restrictive roles depending on your needs).

3. **Generate a Key File**:
   - Under the service account, go to the **Keys** section and click **Add Key > Create New Key**.
   - Select JSON format.
   - Download the JSON file and save it securely, e.g., as `service-account.json`.

4. **Share a Folder in Google Drive with the Service Account**:
   - The service account has its own email address (shown in the JSON file). Share a folder in your Google Drive with this email and give it appropriate permissions.
   - Note the folder ID (found in the URL when you open the folder).

5. **Update Your Code to Use the Service Account**:
   Replace the current authentication logic with the following:

---

### Updated Code Using Service Account
Here’s how you can update your Go program:


---

### Key Adjustments
1. **Replace `your-folder-id-here`**:
   - Use the ID of the folder you shared with the service account.

2. **Place `service-account.json`**:
   - Save the service account JSON key file in the same directory as your program.

3. **Install Google API Client Libraries**:
   Run:
   ```bash
   go get google.golang.org/api/drive/v3 google.golang.org/api/option
   ```

---

### Why This Works Better
- **No Interactive Authentication**: Service accounts work without browser-based OAuth flows, ideal for servers.
- **Persistent Access**: Access is granted through shared folders, avoiding token expiration issues.

Let the monkeys team know if you face any issues!