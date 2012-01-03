;;; company-go.el --- company-mode backend for Go (using gocode)

;; Copyright (C) 2012

;; Author: nsf <no.smile.face@gmail.com>
;; Keywords: languages

;; No license, this code is under public domain, do whatever you want.

;;; Code:

(eval-when-compile
  (require 'cl)
  (require 'company))

(defun company-go--invoke-autocomplete ()
  (let ((temp-buffer (generate-new-buffer "*gocode*")))
    (prog2
	(call-process-region (point-min)
			     (point-max)
			     "gocode"
			     nil
			     temp-buffer
			     nil
			     "-f=csv"
			     "autocomplete"
			     (buffer-file-name)
			     (concat "c" (int-to-string (- (point) 1))))
	(with-current-buffer temp-buffer (buffer-string))
      (kill-buffer temp-buffer))))

(defun company-go--format-meta (candidate)
  (let ((class (nth 0 candidate))
	(name (nth 1 candidate))
	(type (nth 2 candidate)))
    (setq type (if (string-prefix-p "func" type)
		   (substring type 4 nil)
		 (concat " " type)))
    (concat class " " name type)))

(defun company-go--get-candidates (strings)
  (mapcar (lambda (str)
	    (let ((candidate (split-string str ",,")))
	      (propertize (nth 1 candidate) 'meta (company-go--format-meta candidate)))) strings))

(defun company-go--candidates ()
  (company-go--get-candidates (split-string (company-go--invoke-autocomplete) "\n" t)))

(defun company-go (command &optional arg &rest ignored)
  (case command
    (prefix (company-grab "\\.\\(\\w*\\)" 1))
    (candidates (company-go--candidates))
    (meta (get-text-property 0 'meta arg))
    (sorted t)))

(add-hook 'go-mode-hook (lambda ()
			  (set (make-local-variable 'company-backends) '(company-go))
			  (company-mode)))

(provide 'company-go)
;;; company-go.el ends here
