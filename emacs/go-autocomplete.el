;;; go-autocomplete.el --- auto-complete-mode backend for go-mode

;; Copyright (C) 2010  

;; Author: Mikhail <tensai@cirno.in> Kuryshev
;; Keywords: languages

;; This program is free software; you can redistribute it and/or modify
;; it under the terms of the GNU General Public License as published by
;; the Free Software Foundation, either version 3 of the License, or
;; (at your option) any later version.

;; This program is distributed in the hope that it will be useful,
;; but WITHOUT ANY WARRANTY; without even the implied warranty of
;; MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
;; GNU General Public License for more details.

;; You should have received a copy of the GNU General Public License
;; along with this program.  If not, see <http://www.gnu.org/licenses/>.

;;; Commentary:

;; Ensure that go-autocomplete in your load-path and add to your ~/.emacs 
;; following line:
;;
;; (require 'go-autocomplete)

;; Also you could setup any combination (for example M-TAB) 
;; for invoking auto-complete:
;;
;; (require 'auto-complete-config)
;; (define-key ac-mode-map (kbd "M-TAB") 'auto-complete)

;;; Code:

(eval-when-compile
  (require 'cl)
  (require 'auto-complete))

;(defvar go-reserved-keywords
;  '("break" "case" "chan" "const" "continue" "default" "defer" "else"
;    "fallthrough" "for" "func" "go" "goto" "if" "import" "interface" 
;    "map" "package" "range" "return" "select" "struct" "switch" "type" "var")
;  "Go reserved keywords.")

(defun ac-comphist-sort (db collection prefix &optional threshold)
;; redefine to disable sorting
  (let (result
        (n 0)
        (total 0)
        (cur 0))
    (setq result (mapcar (lambda (a)
                           (when (and cur threshold)
                             (if (>= cur (* total threshold))
                                 (setq cur nil)
                               (incf n)
                               (incf cur (cdr a))))
                           (car a))
                         (mapcar (lambda (string)
				   (let ((score (ac-comphist-score db string prefix)))
				     (incf total score)
				     (cons string score)))
				 collection)))
    (if threshold
        (cons n result)
      result)))

(defun ac-go-invoke-autocomplete ()
  (let ((temp-buffer (generate-new-buffer "*gocode*")))
    (prog2
	(call-process-region (point-min)
			     (point-max)
			     "gocode"
			     nil
			     temp-buffer
			     nil
			     "-f=emacs"
			     "autocomplete"
			     (buffer-file-name)
			     (int-to-string (- (point) 1)))
	(with-current-buffer temp-buffer (buffer-string))
      (kill-buffer temp-buffer))))

(defun ac-go-format-autocomplete (buffer-contents)
  (sort
   (split-string buffer-contents "\n")
   '(lambda (a b) (string< (downcase a)
			   (downcase b)))))

(defun ac-go-get-candidates (strings)
  (let ((prop (lambda (entry)
		(let ((name (nth 0 entry))
		      (summary (nth 1 entry)))
		  (propertize name
			      'summary summary))))
	(split (lambda (strings)
		 (mapcar (lambda (str)
			   (split-string str ",,"))
			 strings))))
    (mapcar prop (funcall split strings))))

(defun ac-go-candidates ()
  (ac-go-get-candidates (ac-go-format-autocomplete (ac-go-invoke-autocomplete))))

(defvar ac-source-go
  '((candidates . ac-go-candidates)
    (prefix . "\\.\\(.*\\)")
    (requires . 0)))

(add-hook 'go-mode-hook '(lambda()
			   (auto-complete-mode 1)
			   (setq ac-sources (append '(ac-source-go) ac-sources))))

(add-to-list 'ac-modes 'go-mode)

(provide 'go-autocomplete)
;;; go-autocomplete.el ends here
