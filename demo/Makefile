# release version in github main branch: make release tag=0.0.4
release: | buf
	@echo "Creating github release"
	@-git checkout main
	@-git add .
	@-git commit -am "released v$(tag)"
	@-git tag -a v$(tag) -m "v$(tag)"
	@-git push origin main
	@-git push origin v$(tag)

buf:
	bun i -g @bufbuild/buf
	# buf mod update ./proto
	# buf dep update ./proto
	buf lint
	buf format -w
	rm -rf gen/
	# buf generate
	buf generate --include-imports
	
build: buf
	buf breaking --against '.git#branch=main'