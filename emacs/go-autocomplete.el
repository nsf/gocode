(eval-when-compile (require 'cl))

(defun ac-go-candidates ()
  (ac-go-autocomplete))

(defvar ac-source-go
  '((candidates . ac-go-candidates)
    (prefix . "\\.\\(.*\\)")
    (requires . 0)))

(defun ac-go-get-candidate-strings (tmpbuf)
  (split-string (with-current-buffer tmpbuf (buffer-string)) "\n"))

(defun ac-go-get-candidates (strs)
  (mapcar (lambda (entry)
	    (let ((name (nth 0 entry))
		  (summary (nth 1 entry)))
	      (propertize name
			  'summary summary)))
	  (mapcar (lambda (str)
		    (split-string str ",,"))
		  strs)))

(defun ac-go-autocomplete ()
  (let ((tmpbuf (generate-new-buffer "*gocode*")))
    (call-process-region (point-min) (point-max) "gocode" nil tmpbuf nil "-f=emacs" "autocomplete" (buffer-file-name) (int-to-string (- (point) 1)))
    (prog1
	(ac-go-get-candidates (ac-go-get-candidate-strings tmpbuf))
      (kill-buffer tmpbuf))))

(add-hook 'go-mode-hook '(lambda()
			   (auto-complete-mode 1)
			   (setq ac-sources '(ac-source-go))))

(provide 'go-autocomplete)
