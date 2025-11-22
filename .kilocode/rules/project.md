# Koito 프로젝트 코딩 가이드라인

이 가이드라인은 Koito 프로젝트의 코드베이스 분석을 기반으로 한 Kilo Code Coding Agents를 위한 코딩 표준과 규칙입니다. 백엔드(Go), 프론트엔드(React/TypeScript), 데이터베이스(PostgreSQL), 그리고 일반적인 개발 관행을 다룹니다.

## Backend (Go)

### 프로젝트 구조
- `engine/` 디렉토리에 HTTP 라우팅과 핸들러 배치
- `internal/` 디렉토리에 외부 공개되지 않는 패키지 배치
- `cmd/` 디렉토리에 애플리케이션 진입점 배치
- Go 모듈 구조 준수 (`go.mod` 루트 레벨)

### 의존성 및 구성
- 모든 구성에 환경 변수 사용 (`internal/cfg/cfg.go`)
- 전역 구성에 스레드 안전 싱글톤 패턴 구현 (`sync.Once`)
- zerolog을 사용한 구조화된 로깅 구현 (`KOITO_ENABLE_STRUCTURED_LOGGING`)
- 적절한 속도 제한과 CORS 정책 설정

### 데이터베이스 계층
- PostgreSQL과 pgx 드라이버 사용
- 별도의 쿼리 파일로 리포지토리 패턴 구현 (`db/queries/`)
- goose를 사용한 데이터베이스 마이그레이션
- 복잡한 조인을 위한 뷰 생성 (예: `artists_with_name`, `releases_with_title`)
- 적절한 외래 키 제약조건과 인덱스 구현
- 자동 정리용 트리거 사용 (예: 고아 릴리스 삭제)

### HTTP 핸들러
- `OptsFromRequest()`를 사용하여 쿼리 파라미터를 구조화된 옵션으로 파싱
- 일관된 오류 처리와 로깅 구현
- CORS, 속도 제한, 검증을 위한 미들웨어와 함께 chi 라우터 사용
- 적절한 HTTP 상태 코드로 JSON 응답 반환

### 코드 품질
- Go 1.24+ 기능 사용
- 포괄적인 단위 테스트 구현
- 요청 범위 작업에 컨텍스트 사용
- Go 네이밍 규칙 준수 (PascalCase for exported, camelCase for unexported)
- 의미 있는 변수명 사용과 복잡한 로직에 주석 추가

## Frontend (React/TypeScript)

### 프로젝트 구조
- 파일 기반 라우팅으로 React Router v7 사용
- 모달, 아이콘 등의 하위 디렉토리로 `app/components/`에 컴포넌트 구성
- vanilla-extract CSS-in-JS를 사용하여 `app/styles/`에 스타일 분리
- `app/hooks/`에 커스텀 React 훅 배치
- `app/providers/`에 React 컨텍스트 프로바이더 배치

### 의존성
- TypeScript 엄격 모드 활성화와 함께 React 19 사용
- 서버 상태 관리에 React Query (@tanstack/react-query) 구현
- 접근성 있는 프리미티브에 Radix UI 컴포넌트 사용
- 일관된 아이콘에 Lucide React 사용
- 타입 안전 CSS 변수에 vanilla-extract 구현

### 스타일링 및 테마
- CSS 커스텀 속성과 함께 `vars.css.ts`에 테마 변수 정의
- 일관된 색상 체계로 `themes.css.ts`에 다중 테마 생성
- 동적 테마에 CSS 커스텀 속성 사용
- "pearl" 테마와 같은 라이트/다크 테마 지원 구현
- 커스텀 테마 통합으로 Tailwind CSS 유틸리티 클래스 사용

### 컴포넌트 패턴
- 훅과 함께 함수형 컴포넌트 사용
- props에 적절한 TypeScript 인터페이스 구현
- 적절한 로딩/오류 상태로 React Query를 사용한 데이터 페칭
- 커스텀 Popup 컴포넌트를 사용한 팝업/툴팁 구현
- 일관된 네이밍: 컴포넌트에 PascalCase, props에 camelCase

### 코드 품질
- 엄격한 TypeScript 구성 활성화
- 경로 매핑 사용 (`~/*` for `app/*`)
- 적절한 오류 경계 구현
- 의미 있는 컴포넌트와 prop 이름 사용
- 복잡한 함수에 JSDoc 주석 추가

## Database (PostgreSQL)

### 스키마 설계
- MusicBrainz ID에 UUID, 내부 ID에 정수 사용
- 기본/보조 구분으로 아티스트, 릴리스, 트랙에 별칭 시스템 구현
- 다대다 관계에 연결 테이블 사용 (artist_releases, artist_tracks)
- 일반적으로 조인된 데이터에 뷰 생성
- 제한된 값에 enum 사용 (예: 사용자 역할)

### 마이그레이션
- 마이그레이션 관리에 goose 사용
- up과 down 마이그레이션 모두 포함
- 텍스트 검색 성능을 위한 적절한 인덱스 추가 (pg_trgm)
- 데이터 무결성을 위한 트리거 구현 (예: cascade deletes)
- 다중 문장 작업에 트랜잭션 사용

### 성능
- 트라이그램 기반 텍스트 검색에 GIN 인덱스 추가
- 적절한 곳에 부분 인덱스 생성 (예: `WHERE is_primary = true`)
- 타임스탬프에 timestamptz와 같은 적절한 데이터 타입 사용
- 적절한 외래 키 제약조건 구현

## General

### 개발 워크플로우
- 일관된 개발 환경에 Docker 사용
- 자동화된 테스트에 GitHub Actions로 CI/CD 구현
- 최적화된 프로덕션 이미지에 다단계 Docker 빌드 사용
- conventional commit 메시지 준수

### 오류 처리
- 스택 전체에 적절한 오류 로깅 구현
- 일관된 오류 응답 형식 사용
- 누락된 이미지, 잘못된 데이터와 같은 엣지 케이스 우아하게 처리

### 보안
- 적절한 입력 검증과 정리 구현
- 구성에 보안 기본값 사용
- 인증과 권한 부여 구현
- 프로덕션에서 HTTPS 사용

### 테스트
- 중요한 비즈니스 로직에 단위 테스트 작성
- 데이터베이스 작업에 통합 테스트 사용
- API 엔드포인트 철저하게 테스트
- 테스트 픽스처와 모의 데이터 포함

### 문서화
- 명확한 README와 API 문서 유지
- 구성 옵션과 환경 변수 문서화
- 복잡한 알고리즘에 코드 주석 포함
- 버전 변경으로 changelog 업데이트 유지
