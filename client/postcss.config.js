module.exports = {
    plugins: [
      require('postcss-prefix-selector')({
        // prefix 옵션은 빈 문자열로 두고 transform 함수에서 조건부로 접두어 적용
        prefix: '',
        transform: function (prefix, selector, prefixedSelector, filePath) {
          // dashboard 관련 CSS 파일이면 .dashboard 접두어 적용
          if (filePath && filePath.indexOf('/assets/css/dashboard/') !== -1) {
            // 이미 접두어가 붙은 선택자는 그대로 반환
            if (selector.startsWith('.dashboard')) return selector;
            return '.dashboard ' + selector;
          }
          // login_form 관련 CSS 파일이면 .login-form 접두어 적용
          if (filePath && filePath.indexOf('/assets/css/login_form/') !== -1) {
            if (selector.startsWith('.login-form')) return selector;
            return '.login-form ' + selector;
          }
          // 그 외의 경우는 변경 없이 그대로 반환
          return selector;
        }
      })
    ]
  };
