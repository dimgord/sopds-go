package server

import (
	"html/template"
	"log"
)

// authTemplates holds parsed auth page templates
var authTemplates *template.Template

func init() {
	var err error
	authTemplates, err = template.New("auth").Parse(authBaseTemplate)
	if err != nil {
		log.Fatalf("Failed to parse auth base template: %v", err)
	}

	// Parse all auth page templates
	for name, content := range authPageTemplates {
		_, err = authTemplates.New(name + ".html").Parse(content)
		if err != nil {
			log.Fatalf("Failed to parse auth template %s: %v", name, err)
		}
	}
}

const authBaseTemplate = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{.T.login}} - {{.SiteTitle}}</title>
    <link rel="icon" type="image/svg+xml" href="/favicon.svg">
    <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
    <style>
        :root {
            --primary: #6366f1;
            --primary-dark: #4f46e5;
            --secondary: #0ea5e9;
            --success: #22c55e;
            --warning: #f59e0b;
            --danger: #ef4444;
            --dark: #1e293b;
            --light: #f8fafc;
            --gray: #64748b;
            --border: #e2e8f0;
            --shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1);
            --radius: 12px;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            color: var(--dark);
            padding: 20px;
        }
        .auth-container {
            background: rgba(255,255,255,0.95);
            backdrop-filter: blur(10px);
            border-radius: var(--radius);
            padding: 40px;
            box-shadow: var(--shadow);
            width: 100%;
            max-width: 420px;
        }
        .auth-header {
            text-align: center;
            margin-bottom: 30px;
        }
        .auth-header h1 {
            font-size: 1.8rem;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 10px;
        }
        .auth-header h1 i { -webkit-text-fill-color: var(--primary); }
        .auth-header p {
            color: var(--gray);
            margin-top: 10px;
        }
        .form-group {
            margin-bottom: 20px;
        }
        .form-group label {
            display: block;
            font-weight: 500;
            margin-bottom: 8px;
            color: var(--dark);
        }
        .form-group input {
            width: 100%;
            padding: 14px 16px;
            border: 2px solid var(--border);
            border-radius: 8px;
            font-size: 1rem;
            transition: all 0.3s;
        }
        .form-group input:focus {
            outline: none;
            border-color: var(--primary);
            box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
        }
        .form-group input.valid {
            border-color: var(--success);
        }
        .form-group input.invalid {
            border-color: var(--danger);
        }
        .form-group .hint {
            font-size: 0.85rem;
            color: var(--gray);
            margin-top: 6px;
        }
        .form-group .error {
            color: var(--danger);
            font-size: 0.85rem;
            margin-top: 6px;
        }
        .password-requirements {
            font-size: 0.85rem;
            margin-top: 8px;
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 4px;
        }
        .password-requirements span {
            color: var(--gray);
            display: flex;
            align-items: center;
            gap: 6px;
        }
        .password-requirements span.valid {
            color: var(--success);
        }
        .password-requirements span i {
            font-size: 0.7rem;
        }
        .password-wrapper {
            position: relative;
        }
        .password-wrapper input {
            padding-right: 45px;
        }
        .password-toggle {
            position: absolute;
            right: 14px;
            top: 50%;
            transform: translateY(-50%);
            background: none;
            border: none;
            color: var(--gray);
            cursor: pointer;
            padding: 4px;
            font-size: 1rem;
        }
        .password-toggle:hover {
            color: var(--primary);
        }
        .btn {
            width: 100%;
            padding: 14px 20px;
            border: none;
            border-radius: 8px;
            font-size: 1rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.3s;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 8px;
        }
        .btn-primary {
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            color: white;
        }
        .btn-primary:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 15px rgba(99, 102, 241, 0.4);
        }
        .btn-primary:disabled {
            opacity: 0.6;
            cursor: not-allowed;
            transform: none;
        }
        .btn-secondary {
            background: var(--light);
            color: var(--dark);
            border: 2px solid var(--border);
        }
        .btn-secondary:hover {
            background: var(--border);
        }
        .auth-links {
            text-align: center;
            margin-top: 20px;
            font-size: 0.9rem;
        }
        .auth-links a {
            color: var(--primary);
            text-decoration: none;
            font-weight: 500;
        }
        .auth-links a:hover {
            text-decoration: underline;
        }
        .divider {
            display: flex;
            align-items: center;
            margin: 25px 0;
            color: var(--gray);
        }
        .divider::before,
        .divider::after {
            content: "";
            flex: 1;
            border-bottom: 1px solid var(--border);
        }
        .divider span {
            padding: 0 15px;
            font-size: 0.85rem;
        }
        .alert {
            padding: 14px 16px;
            border-radius: 8px;
            margin-bottom: 20px;
            font-size: 0.9rem;
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .alert-error {
            background: #fef2f2;
            color: var(--danger);
            border: 1px solid #fecaca;
        }
        .alert-success {
            background: #f0fdf4;
            color: var(--success);
            border: 1px solid #bbf7d0;
        }
        .alert-warning {
            background: #fffbeb;
            color: var(--warning);
            border: 1px solid #fde68a;
        }
        .lang-switch {
            position: fixed;
            top: 20px;
            right: 20px;
            display: flex;
            gap: 8px;
        }
        .lang-switch a {
            padding: 8px 12px;
            background: rgba(255,255,255,0.9);
            border-radius: 20px;
            color: var(--dark);
            text-decoration: none;
            font-size: 0.85rem;
            font-weight: 500;
            transition: all 0.3s;
        }
        .lang-switch a:hover,
        .lang-switch a.active {
            background: var(--primary);
            color: white;
        }
        /* Landing page specific */
        .landing-container {
            max-width: 500px;
        }
        .landing-logo {
            font-size: 4rem;
            margin-bottom: 20px;
        }
        .landing-actions {
            display: flex;
            flex-direction: column;
            gap: 12px;
            margin-top: 30px;
        }
    </style>
</head>
<body>
    <div class="lang-switch">
        <a href="?lang=en" class="{{if eq .Lang "en"}}active{{end}}">EN</a>
        <a href="?lang=uk" class="{{if eq .Lang "uk"}}active{{end}}">UK</a>
    </div>
    {{template "content" .}}
</body>
</html>
`

var authPageTemplates = map[string]string{
	"landing": `
{{define "content"}}
<div class="auth-container landing-container">
    <div class="auth-header">
        <div class="landing-logo"><i class="fas fa-book-open"></i></div>
        <h1>{{.T.welcome}} {{.SiteTitle}}</h1>
        <p>{{.T.library_description}}</p>
    </div>
    <div class="landing-actions">
        <a href="{{.WebPrefix}}/login" class="btn btn-primary">
            <i class="fas fa-sign-in-alt"></i> {{.T.login}}
        </a>
        <a href="{{.WebPrefix}}/register" class="btn btn-secondary">
            <i class="fas fa-user-plus"></i> {{.T.register}}
        </a>
        <div class="divider"><span>{{.T.or}}</span></div>
        <form action="{{.WebPrefix}}/guest" method="POST">
            <button type="submit" class="btn btn-secondary">
                <i class="fas fa-user-secret"></i> {{.T.continue_as_guest}}
            </button>
        </form>
    </div>
</div>
{{end}}
`,

	"login": `
{{define "content"}}
<div class="auth-container">
    <div class="auth-header">
        <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
        <p>{{.T.login}}</p>
    </div>

    {{if .Error}}
    <div class="alert alert-error">
        <i class="fas fa-exclamation-circle"></i> {{.Error}}
    </div>
    {{end}}

    {{if .Registered}}
    <div class="alert alert-success">
        <i class="fas fa-check-circle"></i> {{.T.register_success}}
    </div>
    {{end}}

    {{if .Verified}}
    <div class="alert alert-success">
        <i class="fas fa-check-circle"></i> {{.T.verify_success}}
    </div>
    {{end}}

    {{if .Reset}}
    <div class="alert alert-success">
        <i class="fas fa-check-circle"></i> {{.T.reset_success}}
    </div>
    {{end}}

    <form method="POST" action="{{.WebPrefix}}/login">
        <div class="form-group">
            <label for="login">{{.T.login_or_username}}</label>
            <input type="text" id="login" name="login" required autofocus>
        </div>

        <div class="form-group">
            <label for="password">{{.T.password}}</label>
            <input type="password" id="password" name="password" required>
        </div>

        <button type="submit" class="btn btn-primary">
            <i class="fas fa-sign-in-alt"></i> {{.T.login}}
        </button>
    </form>

    <div class="auth-links">
        <a href="{{.WebPrefix}}/forgot-password">{{.T.forgot_password}}</a>
    </div>

    <div class="divider"><span>{{.T.or}}</span></div>

    <div class="auth-links">
        {{.T.no_account}} <a href="{{.WebPrefix}}/register">{{.T.register}}</a>
    </div>
</div>
{{end}}
`,

	"register": `
{{define "content"}}
<div class="auth-container">
    <div class="auth-header">
        <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
        <p>{{.T.register}}</p>
    </div>

    {{if .Error}}
    <div class="alert alert-error">
        <i class="fas fa-exclamation-circle"></i> {{.Error}}
    </div>
    {{end}}

    <form method="POST" action="{{.WebPrefix}}/register" id="registerForm">
        <div class="form-group">
            <label for="username">{{.T.username}}</label>
            <input type="text" id="username" name="username" required
                   pattern="^[a-zA-Z0-9_]{3,30}$" minlength="3" maxlength="30">
            <div class="hint">{{.T.username_requirements}}</div>
            <div class="error" id="usernameError"></div>
        </div>

        <div class="form-group">
            <label for="email">{{.T.email}}</label>
            <input type="email" id="email" name="email" required>
            <div class="error" id="emailError"></div>
        </div>

        <div class="form-group">
            <label for="password">{{.T.password}}</label>
            <div class="password-wrapper">
                <input type="password" id="password" name="password" required minlength="8">
                <button type="button" class="password-toggle" onclick="togglePassword('password', this)">
                    <i class="fas fa-eye"></i>
                </button>
            </div>
            <div class="password-requirements">
                <span id="reqLength"><i class="fas fa-circle"></i> {{.T.req_length}}</span>
                <span id="reqLower"><i class="fas fa-circle"></i> {{.T.req_lower}}</span>
                <span id="reqUpper"><i class="fas fa-circle"></i> {{.T.req_upper}}</span>
                <span id="reqDigit"><i class="fas fa-circle"></i> {{.T.req_digit}}</span>
            </div>
        </div>

        <div class="form-group">
            <label for="confirmPassword">{{.T.confirm_password}}</label>
            <div class="password-wrapper">
                <input type="password" id="confirmPassword" name="confirmPassword" required minlength="8">
                <button type="button" class="password-toggle" onclick="togglePassword('confirmPassword', this)">
                    <i class="fas fa-eye"></i>
                </button>
            </div>
            <div class="error" id="confirmError"></div>
        </div>

        <button type="submit" class="btn btn-primary" id="submitBtn" disabled>
            <i class="fas fa-user-plus"></i> {{.T.register}}
        </button>
    </form>

    <div class="divider"><span>{{.T.or}}</span></div>

    <div class="auth-links">
        {{.T.already_have_account}} <a href="{{.WebPrefix}}/login">{{.T.login}}</a>
    </div>
</div>

<script>
const apiBase = '/api/auth';
let usernameValid = false;
let emailValid = false;
let passwordValid = false;
let confirmValid = false;
let checkTimeout;

function updateSubmitButton() {
    document.getElementById('submitBtn').disabled = !(usernameValid && emailValid && passwordValid && confirmValid);
}

function togglePassword(inputId, btn) {
    const input = document.getElementById(inputId);
    const icon = btn.querySelector('i');
    if (input.type === 'password') {
        input.type = 'text';
        icon.classList.remove('fa-eye');
        icon.classList.add('fa-eye-slash');
    } else {
        input.type = 'password';
        icon.classList.remove('fa-eye-slash');
        icon.classList.add('fa-eye');
    }
}

function validateConfirmPassword() {
    const password = document.getElementById('password').value;
    const confirm = document.getElementById('confirmPassword').value;
    const input = document.getElementById('confirmPassword');
    const error = document.getElementById('confirmError');

    if (confirm.length === 0) {
        input.classList.remove('valid', 'invalid');
        error.textContent = '';
        confirmValid = false;
    } else if (password === confirm) {
        input.classList.remove('invalid');
        input.classList.add('valid');
        error.textContent = '';
        confirmValid = true;
    } else {
        input.classList.remove('valid');
        input.classList.add('invalid');
        error.textContent = 'Passwords do not match';
        confirmValid = false;
    }
    updateSubmitButton();
}

// Username validation
document.getElementById('username').addEventListener('input', function() {
    clearTimeout(checkTimeout);
    const username = this.value.trim();
    const input = this;
    const error = document.getElementById('usernameError');

    if (username.length < 3) {
        input.classList.remove('valid', 'invalid');
        error.textContent = '';
        usernameValid = false;
        updateSubmitButton();
        return;
    }

    checkTimeout = setTimeout(() => {
        fetch(apiBase + '/check-username?username=' + encodeURIComponent(username))
            .then(r => r.json())
            .then(data => {
                if (data.available) {
                    input.classList.remove('invalid');
                    input.classList.add('valid');
                    error.textContent = '';
                    usernameValid = true;
                } else {
                    input.classList.remove('valid');
                    input.classList.add('invalid');
                    error.textContent = data.error || 'Username not available';
                    usernameValid = false;
                }
                updateSubmitButton();
            });
    }, 300);
});

// Email validation
document.getElementById('email').addEventListener('input', function() {
    clearTimeout(checkTimeout);
    const email = this.value.trim();
    const input = this;
    const error = document.getElementById('emailError');

    if (!email.includes('@')) {
        input.classList.remove('valid', 'invalid');
        error.textContent = '';
        emailValid = false;
        updateSubmitButton();
        return;
    }

    checkTimeout = setTimeout(() => {
        fetch(apiBase + '/check-email?email=' + encodeURIComponent(email))
            .then(r => r.json())
            .then(data => {
                if (data.available) {
                    input.classList.remove('invalid');
                    input.classList.add('valid');
                    error.textContent = '';
                    emailValid = true;
                } else {
                    input.classList.remove('valid');
                    input.classList.add('invalid');
                    error.textContent = data.error || 'Email not available';
                    emailValid = false;
                }
                updateSubmitButton();
            });
    }, 300);
});

// Password validation
document.getElementById('password').addEventListener('input', function() {
    const password = this.value;
    const input = this;

    const hasLength = password.length >= 8;
    const hasLower = /[a-z]/.test(password);
    const hasUpper = /[A-Z]/.test(password);
    const hasDigit = /[0-9]/.test(password);

    document.getElementById('reqLength').classList.toggle('valid', hasLength);
    document.getElementById('reqLower').classList.toggle('valid', hasLower);
    document.getElementById('reqUpper').classList.toggle('valid', hasUpper);
    document.getElementById('reqDigit').classList.toggle('valid', hasDigit);

    passwordValid = hasLength && hasLower && hasUpper && hasDigit;

    if (password.length > 0) {
        input.classList.toggle('valid', passwordValid);
        input.classList.toggle('invalid', !passwordValid);
    } else {
        input.classList.remove('valid', 'invalid');
    }

    validateConfirmPassword();
    updateSubmitButton();
});

// Confirm password validation
document.getElementById('confirmPassword').addEventListener('input', validateConfirmPassword);
</script>
{{end}}
`,

	"forgot-password": `
{{define "content"}}
<div class="auth-container">
    <div class="auth-header">
        <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
        <p>{{.T.forgot_password}}</p>
    </div>

    {{if .Error}}
    <div class="alert alert-error">
        <i class="fas fa-exclamation-circle"></i> {{.Error}}
    </div>
    {{end}}

    {{if .Success}}
    <div class="alert alert-success">
        <i class="fas fa-check-circle"></i> {{.Success}}
    </div>
    {{end}}

    <form method="POST" action="{{.WebPrefix}}/forgot-password">
        <div class="form-group">
            <label for="email">{{.T.email}}</label>
            <input type="email" id="email" name="email" required autofocus>
        </div>

        <button type="submit" class="btn btn-primary">
            <i class="fas fa-paper-plane"></i> {{.T.send_reset_link}}
        </button>
    </form>

    <div class="auth-links" style="margin-top: 20px;">
        <a href="{{.WebPrefix}}/login"><i class="fas fa-arrow-left"></i> {{.T.login}}</a>
    </div>
</div>
{{end}}
`,

	"reset-password": `
{{define "content"}}
<div class="auth-container">
    <div class="auth-header">
        <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
        <p>{{.T.reset_password}}</p>
    </div>

    {{if .Error}}
    <div class="alert alert-error">
        <i class="fas fa-exclamation-circle"></i> {{.Error}}
    </div>
    {{end}}

    <form method="POST" action="{{.WebPrefix}}/reset-password?token={{.Token}}" id="resetForm">
        <div class="form-group">
            <label for="password">{{.T.password}}</label>
            <input type="password" id="password" name="password" required minlength="8">
            <div class="password-requirements">
                <span id="reqLength"><i class="fas fa-circle"></i> {{.T.req_length}}</span>
                <span id="reqLower"><i class="fas fa-circle"></i> {{.T.req_lower}}</span>
                <span id="reqUpper"><i class="fas fa-circle"></i> {{.T.req_upper}}</span>
                <span id="reqDigit"><i class="fas fa-circle"></i> {{.T.req_digit}}</span>
            </div>
        </div>

        <button type="submit" class="btn btn-primary" id="submitBtn" disabled>
            <i class="fas fa-key"></i> {{.T.reset_password}}
        </button>
    </form>
</div>

<script>
document.getElementById('password').addEventListener('input', function() {
    const password = this.value;
    const input = this;

    const hasLength = password.length >= 8;
    const hasLower = /[a-z]/.test(password);
    const hasUpper = /[A-Z]/.test(password);
    const hasDigit = /[0-9]/.test(password);

    document.getElementById('reqLength').classList.toggle('valid', hasLength);
    document.getElementById('reqLower').classList.toggle('valid', hasLower);
    document.getElementById('reqUpper').classList.toggle('valid', hasUpper);
    document.getElementById('reqDigit').classList.toggle('valid', hasDigit);

    const valid = hasLength && hasLower && hasUpper && hasDigit;

    if (password.length > 0) {
        input.classList.toggle('valid', valid);
        input.classList.toggle('invalid', !valid);
    } else {
        input.classList.remove('valid', 'invalid');
    }

    document.getElementById('submitBtn').disabled = !valid;
});
</script>
{{end}}
`,

	"message": `
{{define "content"}}
<div class="auth-container">
    <div class="auth-header">
        <h1><i class="fas fa-book-open"></i> {{.SiteTitle}}</h1>
        <p>{{.Title}}</p>
    </div>

    <div class="alert alert-warning">
        <i class="fas fa-info-circle"></i> {{.Message}}
    </div>

    <a href="{{.WebPrefix}}/login" class="btn btn-primary">
        <i class="fas fa-sign-in-alt"></i> {{.T.login}}
    </a>
</div>
{{end}}
`,
}
